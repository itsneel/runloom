export function secretNameError(name: string): string {
  if (!name) return 'Enter a secret name.';
  if (/\s/.test(name)) return 'Spaces are not allowed in secret names. Use a hyphen instead, for example billing-api.';
  if (name.length > 64) return 'Use 64 characters or fewer for the secret name.';
  if (!/^[a-z][a-z0-9-]*$/.test(name))
    return 'Start with a lowercase letter and use only lowercase letters, digits and hyphens.';
  return '';
}

export function secretValueError(value: string): string {
  if (!value) return 'Enter a secret value.';
  if (/[\r\n\u0000]/.test(value)) return 'The secret value cannot contain newlines or null characters.';
  if (new TextEncoder().encode(value).length > 8192) return 'The secret value must be 8,192 bytes or fewer.';
  return '';
}
