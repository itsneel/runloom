export function parsePromptTags(value: string): string[] {
  return [...new Set(value.split(',').map(tag => tag.trim().toLowerCase()).filter(Boolean))];
}

export function promptTagsError(tags: string[]): string | undefined {
  if (tags.length > 20) return 'Use up to 20 tags per prompt.';
  if (tags.some(tag => !tag.trim() || [...tag].length > 40 || /[,\u0000-\u001f\u007f-\u009f]/u.test(tag)))
    return 'Each tag must contain 1–40 characters, without commas or control characters.';
  return undefined;
}
