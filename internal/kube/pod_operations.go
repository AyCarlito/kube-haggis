package kube

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	// maxPodCopySize bounds one browser-to-container or container-to-browser file transfer.
	maxPodCopySize int64 = 64 << 20
	// maxPodCopyPathLength limits a remote POSIX path accepted by the copy API.
	maxPodCopyPathLength = 4096
)

// EphemeralContainerRequest contains the supported fields for one debug container.
type EphemeralContainerRequest struct {
	Name                string
	Image               string
	Command             []string
	TargetContainerName string
}

// podCopyPath contains the normalized remote directory and file name for tar.
type podCopyPath struct {
	directory string
	name      string
}

// AddEphemeralContainer appends a debug container through the Pod ephemeralcontainers subresource.
func (s *Service) AddEphemeralContainer(ctx context.Context, contextName, namespace, podName string, request EphemeralContainerRequest) (*corev1.Pod, error) {
	namespace = strings.TrimSpace(namespace)
	podName = strings.TrimSpace(podName)
	request.Name = strings.TrimSpace(request.Name)
	request.Image = strings.TrimSpace(request.Image)
	request.TargetContainerName = strings.TrimSpace(request.TargetContainerName)
	if err := validateEphemeralContainerRequest(namespace, podName, request); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	clientset, _, err := s.typedClient(contextName)
	if err != nil {
		return nil, err
	}
	pods := clientset.CoreV1().Pods(namespace)
	pod, err := pods.Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, describePodReadError("read Pod for ephemeral debug container", err)
	}
	if request.TargetContainerName != "" && !podHasRegularContainer(pod, request.TargetContainerName) {
		return nil, fmt.Errorf("target container %q was not found in Pod %q", request.TargetContainerName, podName)
	}
	// ManagedFields are server-owned and must not be sent back in the mutation payload.
	pod.ManagedFields = nil
	for _, existing := range pod.Spec.EphemeralContainers {
		if existing.Name == request.Name {
			return nil, fmt.Errorf("ephemeral container %q already exists in Pod %q", request.Name, podName)
		}
	}

	pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, corev1.EphemeralContainer{
		EphemeralContainerCommon: corev1.EphemeralContainerCommon{
			Name:    request.Name,
			Image:   request.Image,
			Command: append([]string(nil), request.Command...),
		},
		TargetContainerName: request.TargetContainerName,
	})
	updated, err := pods.UpdateEphemeralContainers(ctx, pod.Name, pod, metav1.UpdateOptions{FieldManager: "kube-haggis"})
	if err != nil {
		return nil, describeEphemeralContainerUpdateError(err)
	}
	return updated, nil
}

// CopyFileFromPod downloads one regular file through the native Pod exec stream.
func (s *Service) CopyFileFromPod(ctx context.Context, contextName, namespace, podName, container, remotePath string) ([]byte, error) {
	copyPath, err := validatePodCopyPath(namespace, podName, container, remotePath)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	clientset, restConfig, err := s.typedClient(contextName)
	if err != nil {
		return nil, err
	}
	executor, err := newPodRemoteExecutor(restConfig, clientset, namespace, podName, container, []string{"tar", "cf", "-", "-C", copyPath.directory, "--", copyPath.name}, false)
	if err != nil {
		return nil, err
	}

	archiveReader, archiveWriter := io.Pipe()
	streamErrors := make(chan error, 1)
	go streamPodArchive(ctx, executor, archiveWriter, streamErrors)

	archive := tar.NewReader(archiveReader)
	header, err := archive.Next()
	if err != nil {
		streamErr := <-streamErrors
		if streamErr != nil {
			return nil, describePodCopyError("download file from Pod", streamErr)
		}
		if err == io.EOF {
			return nil, fmt.Errorf("download file from Pod: remote path %q was not returned", remotePath)
		}
		return nil, fmt.Errorf("download file from Pod: invalid tar stream: %w", err)
	}
	if !isRegularTarEntry(header) {
		_ = archiveReader.CloseWithError(fmt.Errorf("remote path %q is not a regular file", remotePath))
		<-streamErrors
		return nil, fmt.Errorf("remote path %q is not a regular file", remotePath)
	}
	if !safeTarEntryName(header.Name) || path.Base(path.Clean(header.Name)) != copyPath.name {
		_ = archiveReader.CloseWithError(fmt.Errorf("remote tar entry is not a safe file name"))
		<-streamErrors
		return nil, fmt.Errorf("download file from Pod: remote tar entry is not a safe file name")
	}
	if header.Size < 0 || header.Size > maxPodCopySize {
		_ = archiveReader.CloseWithError(fmt.Errorf("downloaded file exceeds %d MiB", maxPodCopySize>>20))
		<-streamErrors
		return nil, fmt.Errorf("downloaded file exceeds %d MiB limit", maxPodCopySize>>20)
	}

	result := bytes.NewBuffer(make([]byte, 0, int(header.Size)))
	if _, err := io.CopyN(result, archive, header.Size); err != nil {
		_ = archiveReader.CloseWithError(err)
		<-streamErrors
		return nil, fmt.Errorf("download file from Pod: read file contents: %w", err)
	}
	if _, err := archive.Next(); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("archive contains more than one file")
		}
		_ = archiveReader.CloseWithError(err)
		<-streamErrors
		return nil, fmt.Errorf("download file from Pod: invalid tar stream: %w", err)
	}
	if err := <-streamErrors; err != nil {
		return nil, describePodCopyError("download file from Pod", err)
	}
	return result.Bytes(), nil
}

// CopyFileToPod uploads one local file through the native Pod exec stream.
func (s *Service) CopyFileToPod(ctx context.Context, contextName, namespace, podName, container, remotePath string, source io.Reader, size int64) error {
	copyPath, err := validatePodCopyPath(namespace, podName, container, remotePath)
	if err != nil {
		return err
	}
	if source == nil {
		return fmt.Errorf("local file content is required for Pod upload")
	}
	if size < 0 || size > maxPodCopySize {
		return fmt.Errorf("upload file must be no larger than %d MiB", maxPodCopySize>>20)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	clientset, restConfig, err := s.typedClient(contextName)
	if err != nil {
		return err
	}
	executor, err := newPodRemoteExecutor(restConfig, clientset, namespace, podName, container, []string{"tar", "xmf", "-", "-C", copyPath.directory}, true)
	if err != nil {
		return err
	}

	archiveReader, archiveWriter := io.Pipe()
	archiveErrors := make(chan error, 1)
	go writePodTarArchive(archiveWriter, source, size, copyPath.name, archiveErrors)
	streamErr := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  archiveReader,
		Stderr: io.Discard,
		Tty:    false,
	})
	if streamErr != nil {
		_ = archiveReader.CloseWithError(streamErr)
	}
	archiveErr := <-archiveErrors
	if streamErr != nil {
		return describePodCopyError("upload file to Pod", streamErr)
	}
	if archiveErr != nil {
		return fmt.Errorf("upload file to Pod: prepare tar stream: %w", archiveErr)
	}
	return nil
}

// typedClient creates the native Kubernetes client used by Pod subresources.
func (s *Service) typedClient(contextName string) (kubernetes.Interface, *rest.Config, error) {
	_, _, restConfig, err := s.clients(contextName)
	if err != nil {
		return nil, nil, err
	}
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("create typed Kubernetes client: %w", err)
	}
	return clientset, restConfig, nil
}

// validateEphemeralContainerRequest checks debug-container coordinates before client creation.
func validateEphemeralContainerRequest(namespace, podName string, request EphemeralContainerRequest) error {
	if namespace == "" || namespace == "all" {
		return fmt.Errorf("namespace is required for ephemeral debug containers")
	}
	if podName == "" {
		return fmt.Errorf("Pod name is required for ephemeral debug containers")
	}
	if request.Name == "" {
		return fmt.Errorf("debug container name is required")
	}
	if len(request.Name) > 63 {
		return fmt.Errorf("debug container name must be no longer than 63 characters")
	}
	if errors := validation.IsDNS1123Label(request.Name); len(errors) > 0 {
		return fmt.Errorf("debug container name is invalid: %s", strings.Join(errors, "; "))
	}
	if request.Image == "" {
		return fmt.Errorf("debug container image is required")
	}
	if strings.ContainsAny(request.Image, "\x00\r\n\t") {
		return fmt.Errorf("debug container image contains invalid control characters")
	}
	for index, argument := range request.Command {
		if strings.TrimSpace(argument) == "" {
			return fmt.Errorf("debug container command argument %d is empty", index+1)
		}
	}
	if request.TargetContainerName != "" {
		if len(request.TargetContainerName) > 63 {
			return fmt.Errorf("target container name must be no longer than 63 characters")
		}
		if errors := validation.IsDNS1123Label(request.TargetContainerName); len(errors) > 0 {
			return fmt.Errorf("target container name is invalid: %s", strings.Join(errors, "; "))
		}
	}
	return nil
}

// validatePodCopyPath checks a remote POSIX path before invoking tar in a container.
func validatePodCopyPath(namespace, podName, container, remotePath string) (podCopyPath, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(namespace) == "all" {
		return podCopyPath{}, fmt.Errorf("namespace is required for Pod file copy")
	}
	if strings.TrimSpace(podName) == "" {
		return podCopyPath{}, fmt.Errorf("Pod name is required for file copy")
	}
	if strings.ContainsAny(container, "\x00\r\n") {
		return podCopyPath{}, fmt.Errorf("container name contains invalid control characters")
	}
	remotePath = strings.TrimSpace(remotePath)
	if remotePath == "" {
		return podCopyPath{}, fmt.Errorf("remote file path is required")
	}
	if len(remotePath) > maxPodCopyPathLength {
		return podCopyPath{}, fmt.Errorf("remote file path must be no longer than %d characters", maxPodCopyPathLength)
	}
	if !strings.HasPrefix(remotePath, "/") {
		return podCopyPath{}, fmt.Errorf("remote file path must be absolute")
	}
	if strings.HasSuffix(remotePath, "/") {
		return podCopyPath{}, fmt.Errorf("remote file path must identify a file, not a directory")
	}
	for _, segment := range strings.Split(remotePath, "/") {
		if segment == "." || segment == ".." {
			return podCopyPath{}, fmt.Errorf("remote file path must not contain . or .. path segments")
		}
		if strings.ContainsAny(segment, "\x00\r\n") {
			return podCopyPath{}, fmt.Errorf("remote file path contains invalid control characters")
		}
	}
	cleanPath := path.Clean(remotePath)
	name := path.Base(cleanPath)
	if name == "." || name == "/" || name == "" {
		return podCopyPath{}, fmt.Errorf("remote file path must identify a file")
	}
	return podCopyPath{directory: path.Dir(cleanPath), name: name}, nil
}

// podHasRegularContainer reports whether a Pod has the requested debug target.
func podHasRegularContainer(pod *corev1.Pod, name string) bool {
	for _, container := range pod.Spec.Containers {
		if container.Name == name {
			return true
		}
	}
	return false
}

// newPodRemoteExecutor creates the native pods/exec executor used by file copy.
func newPodRemoteExecutor(restConfig *rest.Config, clientset kubernetes.Interface, namespace, podName, container string, command []string, stdin bool) (remotecommand.Executor, error) {
	request := clientset.CoreV1().RESTClient().Post().Resource("pods").Namespace(namespace).Name(podName).SubResource("exec")
	request.VersionedParams(&corev1.PodExecOptions{
		Container: container,
		Command:   command,
		Stdin:     stdin,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, scheme.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(restConfig, http.MethodPost, request.URL())
	if err != nil {
		return nil, fmt.Errorf("create native Pod exec stream: %w", err)
	}
	return executor, nil
}

// streamPodArchive copies native exec stdout into a pipe consumed by the tar reader.
func streamPodArchive(ctx context.Context, executor remotecommand.Executor, writer *io.PipeWriter, result chan<- error) {
	err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: writer, Stderr: io.Discard, Tty: false})
	if err != nil {
		_ = writer.CloseWithError(err)
	} else {
		_ = writer.Close()
	}
	result <- err
}

// writePodTarArchive creates the single-file tar stream consumed by a container.
func writePodTarArchive(writer *io.PipeWriter, source io.Reader, size int64, name string, result chan<- error) {
	var err error
	tarWriter := tar.NewWriter(writer)
	if err = tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: size, ModTime: time.Now().UTC()}); err == nil {
		var copied int64
		copied, err = io.CopyN(tarWriter, source, size)
		if err == nil && copied != size {
			err = fmt.Errorf("local file ended after %d of %d bytes", copied, size)
		}
	}
	if err == nil {
		err = tarWriter.Close()
	}
	if err != nil {
		_ = writer.CloseWithError(err)
	} else {
		_ = writer.Close()
	}
	result <- err
}

// isRegularTarEntry accepts the two tar type codes used for regular files.
func isRegularTarEntry(header *tar.Header) bool {
	return header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA
}

// safeTarEntryName rejects archive entries that could escape the requested file.
func safeTarEntryName(name string) bool {
	if name == "" || path.IsAbs(name) {
		return false
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == ".." {
			return false
		}
	}
	return true
}

// describePodReadError turns Pod lookup authorization failures into operator-facing errors.
func describePodReadError(operation string, err error) error {
	switch {
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		return fmt.Errorf("%s was denied by Kubernetes RBAC: %w", operation, err)
	case apierrors.IsNotFound(err):
		return fmt.Errorf("%s: Pod was not found: %w", operation, err)
	default:
		return fmt.Errorf("%s failed: %w", operation, err)
	}
}

// describeEphemeralContainerUpdateError explains feature and permission failures from the subresource.
func describeEphemeralContainerUpdateError(err error) error {
	switch {
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		return fmt.Errorf("creating ephemeral debug containers was denied by Kubernetes RBAC: %w", err)
	case apierrors.IsNotFound(err), apierrors.IsMethodNotSupported(err), apierrors.IsNotAcceptable(err):
		return fmt.Errorf("ephemeral debug containers are not supported by this Kubernetes API server (pods/ephemeralcontainers): %w", err)
	case apierrors.IsConflict(err):
		return fmt.Errorf("the Pod changed while adding the ephemeral debug container; refresh and try again: %w", err)
	default:
		return fmt.Errorf("update Pod ephemeral debug container failed: %w", err)
	}
}

// describePodCopyError explains native exec permission and transport failures without returning command output.
func describePodCopyError(operation string, err error) error {
	message := strings.ToLower(err.Error())
	if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) || strings.Contains(message, "forbidden") || strings.Contains(message, "unauthorized") {
		return fmt.Errorf("%s was denied by Kubernetes RBAC (requires pods/exec): %w", operation, err)
	}
	if apierrors.IsNotFound(err) || strings.Contains(message, "not found") {
		return fmt.Errorf("%s could not find the Pod or native exec subresource: %w", operation, err)
	}
	return fmt.Errorf("%s failed through the native Pod exec stream: %w", operation, err)
}
