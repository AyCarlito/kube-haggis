import { parseSchemaInput, projectSchemaIdentityFields, validateSchemaScalarValue } from './schemaForm'
import type { ResourceSchemaField } from './types'

/** Fails a focused schema-form model assertion with a readable message. */
function assertSchemaForm(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Exercises root metadata identity projection without mounting a React form. */
export function runSchemaFormModelTests(): void {
  const schema: ResourceSchemaField = {
    type: 'object',
    properties: [
      {
        name: 'metadata',
        type: 'object',
        properties: [
          { name: 'name', type: 'string', readOnly: true },
          { name: 'namespace', type: 'string', readOnly: true },
          { name: 'generateName', type: 'string' },
          { name: 'labels', type: 'object', additionalProperties: { type: 'string' } },
          { name: 'annotations', type: 'object', additionalProperties: { type: 'string' } },
        ],
      },
      { name: 'spec', type: 'object', properties: [{ name: 'replicas', type: 'integer' }] },
    ],
  }
  const projected = projectSchemaIdentityFields(schema)
  const metadata = projected.properties?.find((property) => property.name === 'metadata')
  const metadataNames = metadata?.properties?.map((property) => property.name) || []
  assertSchemaForm(!metadataNames.includes('name') && !metadataNames.includes('namespace'), 'identity metadata fields remained in the generic schema tree')
  assertSchemaForm(metadataNames.includes('generateName') && metadataNames.includes('labels') && metadataNames.includes('annotations'), 'metadata details were incorrectly projected to YAML-only')
  assertSchemaForm(projected.properties?.find((property) => property.name === 'spec')?.properties?.some((property) => property.name === 'replicas') === true, 'non-metadata schema fields were changed by identity projection')
  assertSchemaForm(schema.properties?.find((property) => property.name === 'metadata')?.properties?.some((property) => property.name === 'name') === true, 'identity projection mutated the source schema')

  const intOrString: ResourceSchemaField = { type: 'string', format: 'int-or-string', scalarTypes: ['string', 'integer'] }
  assertSchemaForm(parseSchemaInput(intOrString, '8080') === 8080, 'numeric IntOrString input was not preserved as a JSON integer')
  assertSchemaForm(parseSchemaInput(intOrString, 'http') === 'http' && parseSchemaInput(intOrString, '50%') === '50%', 'named and percentage IntOrString values were not preserved as strings')
  assertSchemaForm(validateSchemaScalarValue(intOrString, 8080, '$.targetPort').length === 0 && validateSchemaScalarValue(intOrString, 'http', '$.targetPort').length === 0 && validateSchemaScalarValue(intOrString, '50%', '$.targetPort').length === 0, 'valid IntOrString variants failed scalar validation')

  const quantity: ResourceSchemaField = { type: 'string', format: 'quantity', scalarTypes: ['string', 'number'] }
  assertSchemaForm(parseSchemaInput(quantity, '250m') === '250m' && parseSchemaInput(quantity, '1Gi') === '1Gi', 'resource.Quantity text was coerced away from canonical Kubernetes text')
  assertSchemaForm(validateSchemaScalarValue(quantity, '250m', '$.divisor').length === 0, 'resource.Quantity text failed scalar validation')
}

runSchemaFormModelTests()
