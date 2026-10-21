import type { ResourceSchemaField } from './types'

/** Projects only root metadata identity fields out of the generic schema form. */
export function projectSchemaIdentityFields(field: ResourceSchemaField): ResourceSchemaField {
  if (field.type !== 'object' || !field.properties) return field
  return {
    ...field,
    properties: field.properties.map((property) => {
      if (property.name !== 'metadata' || property.type !== 'object' || !property.properties) return property
      return {
        ...property,
        properties: property.properties.filter((metadataField) => metadataField.name !== 'name' && metadataField.name !== 'namespace'),
      }
    }),
  }
}

/** Converts scalar form text while preserving the JSON variants declared by the normalized schema. */
export function parseSchemaInput(field: ResourceSchemaField, value: string): unknown {
  if (value === '') return undefined
  if (field.scalarTypes?.includes('integer') && field.scalarTypes.includes('string')) {
    if (/^\d+$/.test(value)) {
      const integer = Number(value)
      if (Number.isSafeInteger(integer)) return integer
    }
    return value
  }
  if (!field.scalarTypes?.length && (field.type === 'integer' || field.type === 'number')) return Number(value)
  return value
}

/** Validates one scalar value against its exact primitive variants and applicable constraints. */
export function validateSchemaScalarValue(field: ResourceSchemaField, value: unknown, path: string): string[] {
  const scalarTypes = field.scalarTypes?.length ? field.scalarTypes : [field.type]
  if (typeof value === 'string' && scalarTypes.includes('string')) {
    const errors: string[] = []
    if (field.minLength !== undefined && value.length < field.minLength) errors.push(`${path} must contain at least ${field.minLength} characters.`)
    if (field.maxLength !== undefined && value.length > field.maxLength) errors.push(`${path} must contain at most ${field.maxLength} characters.`)
    if (field.pattern) {
      try {
        if (!new RegExp(field.pattern).test(value)) errors.push(`${path} does not match the required pattern.`)
      } catch {
        errors.push(`${path} uses an invalid validation pattern.`)
      }
    }
    return errors
  }
  const acceptsInteger = scalarTypes.includes('integer')
  const acceptsNumber = scalarTypes.includes('number')
  if (typeof value === 'number' && Number.isFinite(value) && ((acceptsInteger && Number.isInteger(value)) || acceptsNumber)) {
    const errors: string[] = []
    if (field.minimum !== undefined && (field.exclusiveMinimum ? value <= field.minimum : value < field.minimum)) errors.push(`${path} is below the minimum.`)
    if (field.maximum !== undefined && (field.exclusiveMaximum ? value >= field.maximum : value > field.maximum)) errors.push(`${path} is above the maximum.`)
    if (field.multipleOf !== undefined && Math.abs(value / field.multipleOf - Math.round(value / field.multipleOf)) > 1e-9) errors.push(`${path} must be a multiple of ${field.multipleOf}.`)
    return errors
  }
  const expected = scalarTypes.length > 1 ? scalarTypes.join(' or ') : scalarTypes[0]
  return [`${path} must be a ${expected}.`]
}
