// Choose a default home only after OIDC callback processing and verified role lookup.
// Explicit deep links remain intact; backend authorization still controls access.
export function loginLanding(path: string, platformRole: string): string | undefined {
  const pathname = path.split(/[?#]/, 1)[0].replace(/\/$/, '');
  if (['platform_admin', 'platform_staff'].includes(platformRole) && ['', '/workflows'].includes(pathname)) {
    return '/platform/health';
  }
  return undefined;
}
