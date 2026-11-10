import { inject, Injectable } from '@angular/core';
import { AuthService } from '@runloom/studio-platform';

export interface Page<T> {
  items: T[];
  total: number;
  offset: number;
  limit: number;
}
export interface OrganizationMetadata {
  id: string;
  name: string;
  team_count: number;
  member_count: number;
  parent_id?: string;
  child_count: number;
  child_limit?: OrganizationChildPolicy;
}
export interface OrganizationChildPolicy {
  max_children: number;
  default_max_children: number;
  override: number | null;
  maximum: number;
}
export interface StaffMember {
  id: string;
  email: string;
  name: string;
  role: string;
}
export interface PlatformEvent {
  actor: string;
  target: string;
  action: string;
  role: string;
  previous_role: string;
  reason: string;
  created_at: string;
}
export interface ServiceStatus {
  service: string;
  status: string;
  detail: string;
  latency_ms: number;
}
export interface Health {
  items: ServiceStatus[];
  checked_at: string;
}

@Injectable({ providedIn: 'root' })
export class PlatformApi {
  private readonly auth = inject(AuthService);
  list<T>(kind: string, offset: number): Promise<Page<T>> {
    return this.auth.request('/api/v1/platform/' + kind + '?limit=25&offset=' + offset);
  }
  health(): Promise<Health> {
    return this.auth.request('/api/v1/platform/health');
  }
  organizationSettings(maxChildren?: number): Promise<{ max_children: number; maximum: number }> {
    return this.auth.request(
      '/api/v1/platform/organization-settings',
      maxChildren === undefined ? undefined : { max_children: maxChildren },
      maxChildren === undefined ? 'GET' : 'PUT',
    );
  }
  organizationLimit(id: string, maxChildren?: number): Promise<OrganizationChildPolicy> {
    return this.auth.request(
      '/api/v1/platform/organizations/' + encodeURIComponent(id) + '/child-limit',
      maxChildren === undefined ? undefined : { max_children: maxChildren },
      maxChildren === undefined ? 'DELETE' : 'PUT',
    );
  }
  assign(email: string, role: string, reason: string): Promise<unknown> {
    return this.auth.request('/api/v1/platform/staff', { email, role, reason }, 'PUT');
  }
}
