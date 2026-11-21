type Property = {
  type?: string;
  title?: string;
  minLength?: number;
  maxLength?: number;
  minimum?: number;
  maximum?: number;
  enum?: unknown[];
};
export type TestField = { key: string; label: string; required: boolean; rule: Property };

export function testFields(schema: Record<string, unknown>): TestField[] {
  const properties = schema?.['properties'];
  if (!properties || typeof properties !== 'object' || Array.isArray(properties)) return [];
  const fields: TestField[] = [];
  for (const [key, rule] of Object.entries(properties)) {
    if (!rule || !['string', 'integer', 'number', 'boolean'].includes(rule.type)) return [];
    fields.push({
      key,
      label: (typeof rule.title === 'string' && rule.title) || key,
      required: Array.isArray(schema['required']) && schema['required'].includes(key),
      rule,
    });
  }
  return fields;
}

export function fieldError(field: TestField, input: Record<string, unknown>): string {
  if (!Object.hasOwn(input, field.key)) return field.required ? 'Enter a value.' : '';
  const value = input[field.key];
  const rule = field.rule;
  if (rule.type === 'integer' && !Number.isInteger(value)) return 'Enter a whole number.';
  if (rule.type !== 'integer' && typeof value !== rule.type)
    return 'Enter a ' + rule.type + ' value.';
  if (typeof value === 'string') {
    const length = [...value].length;
    if (rule.minLength !== undefined && length < rule.minLength)
      return 'Enter at least ' + rule.minLength + ' characters.';
    if (rule.maxLength !== undefined && length > rule.maxLength)
      return 'Use at most ' + rule.maxLength + ' characters.';
  }
  if (typeof value === 'number') {
    if (rule.minimum !== undefined && value < rule.minimum)
      return 'Enter ' + rule.minimum + ' or more.';
    if (rule.maximum !== undefined && value > rule.maximum)
      return 'Enter ' + rule.maximum + ' or less.';
  }
  if (Array.isArray(rule.enum) && !rule.enum.includes(value))
    return 'Choose one of: ' + rule.enum.join(', ') + '.';
  return '';
}

export function editTestField(
  input: Record<string, unknown>,
  field: TestField,
  text: string,
): string {
  const next = { ...input };
  let value: string | boolean | number = text;
  if (field.rule.type === 'boolean' && ['true', 'false'].includes(text)) value = text === 'true';
  if (
    ['integer', 'number'].includes(field.rule.type ?? '') &&
    /^-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?$/.test(text) &&
    Number.isFinite(Number(text))
  )
    value = Number(text);
  if (!text) delete next[field.key];
  else Object.defineProperty(next, field.key, { value, enumerable: true, configurable: true });
  return JSON.stringify(next, null, 2);
}
