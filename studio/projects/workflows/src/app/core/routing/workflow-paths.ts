// Public URLs hosted by Studio's router. No host source dependency is required.
export function projectUrl(id: string): string {
  return '/workflows/projects/' + encodeURIComponent(id);
}

export function definitionUrl(id: string): string {
  return '/workflows/definitions/' + encodeURIComponent(id);
}
