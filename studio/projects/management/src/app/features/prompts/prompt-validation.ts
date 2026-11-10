import type { PromptEntry } from './models/prompt.models';

export function suggestedPromptID(name: string, suffix: string): string {
  let stem = name.normalize('NFKD').replace(/[\u0300-\u036f]/g, '').toLowerCase()
    .replace(/[^a-z0-9_-]+/g, '-').replace(/^-+|-+$/g, '');
  if (!/^[a-z]/.test(stem)) stem = 'prompt-' + stem;
  return `${stem.slice(0, 55).replace(/-+$/g, '')}-${suffix}`;
}

export function promptFieldErrors(entry?: PromptEntry): Record<string, string> {
  if (!entry) return {};
  const errors: Record<string, string> = {};
  const bytes = (value: string) => new TextEncoder().encode(value).length;
  if (!/^[a-z][a-z0-9_-]{0,63}$/.test(entry.id))
    errors['id'] = 'Use an ID starting with a lowercase letter, followed by lowercase letters, digits, hyphens or underscores (up to 64 characters). Spaces are not allowed.';
  if (!entry.name.trim() || bytes(entry.name) > 120)
    errors['name'] = 'Enter a prompt name of up to 120 bytes.';
  if (!entry.template.text.trim() || bytes(entry.template.text) > 16000)
    errors['text'] = 'Enter the prompt text (up to 16,000 bytes) before saving or publishing.';
  if (bytes(entry.template.instructions) > 16000)
    errors['instructions'] = 'System instructions must be 16,000 bytes or fewer.';
  return errors;
}
