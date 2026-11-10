// Shared terminal presentation for local setup helpers, not application code.
// Values are public development defaults, never retrieved account passwords.
export function localAccountTable(accounts, origin) {
  const entries = [accounts, accounts?.member, accounts?.platform_admin, accounts?.platform_staff];
  for (const entry of entries) {
    if (!entry || typeof entry.email !== 'string' || typeof entry.default_password !== 'string' || typeof entry.created !== 'boolean') {
      throw new Error('Incomplete local account setup result. Rebuild the admin setup image.');
    }
  }
  for (const entry of entries.slice(2)) {
    if (!['', 'platform_admin', 'platform_staff'].includes(entry.role)) throw new Error('Invalid local platform account role.');
  }
  const base = origin.replace(/\/$/, '');
  const rows = [
    ['Email / username', 'Role', 'Organization', 'Team', 'Local default password'],
    [accounts.email, 'owner / admin', accounts.organization || 'Runloom', accounts.team || 'General', accounts.default_password],
    [accounts.member.email, `${accounts.member.default_org_role} / ${accounts.member.default_team_role}`, accounts.organization || 'Runloom', accounts.team || 'General', accounts.member.default_password],
    ...entries.slice(2).map(entry => [entry.email, entry.role || 'none (revoked)', '-', '-', entry.default_password]),
    ['admin', 'Keycloak admin', '-', '-', 'development-only'],
  ].map(row => row.map(value => String(value).replace(/[\x00-\x1f\x7f-\x9f]/g, ' ')));
  const widths = rows[0].map((_, i) => Math.max(...rows.map(row => row[i].length)));
  const border = '+' + widths.map(width => '-'.repeat(width + 2)).join('+') + '+';
  const line = row => '| ' + row.map((value, i) => value.padEnd(widths[i])).join(' | ') + ' |';
  const table = [border, line(rows[0]), border, ...rows.slice(1).map(line), border].join('\n');
  const passwordNote = entries.some(entry => entry.reset)
    ? 'Explicit reset: Runloom local default passwords restored and existing sessions ended. Keycloak credentials are unchanged.'
    : 'Existing passwords are preserved; displayed defaults work only if unchanged. New accounts use these defaults.';
  return `${table}\nStudio: ${base}/workflows | Platform console: ${base}/platform\nKeycloak admin: ${base}/auth/admin/master/console/ | Login realm: master; select runloom to manage the product.\nLocal development only. ${passwordNote}\nTenant roles shown are setup defaults; existing member roles and dedicated platform access decisions are preserved.`;
}
