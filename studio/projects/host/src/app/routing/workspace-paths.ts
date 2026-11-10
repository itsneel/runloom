import type { ManagementSection } from '@runloom/studio-platform';

export const managementPaths: Record<ManagementSection, string> = {
  teams: 'teams',
  organization: 'organization',
  accounts: 'service-accounts',
  secrets: 'secrets',
  connections: 'connections',
  analytics: 'analytics',
  budgets: 'budgets',
  prompts: 'prompts',
  runs: 'runs',
  schedules: 'schedules',
};

export function managementUrl(section: ManagementSection): string {
  return '/management/' + managementPaths[section];
}
