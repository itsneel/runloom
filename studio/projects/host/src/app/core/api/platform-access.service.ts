import { inject, Injectable, signal } from '@angular/core';
import { AuthService } from '@runloom/studio-platform';

export interface PlatformIdentity { role: string; permissions: string[]; }

@Injectable({ providedIn: 'root' })
export class PlatformAccess {
  private readonly auth = inject(AuthService);
  readonly identity = signal<PlatformIdentity>({ role: '', permissions: [] });
  async refresh(): Promise<PlatformIdentity> {
    try {
      const identity = await this.auth.request<PlatformIdentity>('/api/v1/platform/me');
      this.identity.set(identity);
      return identity;
    } catch (error) {
      this.identity.set({ role: '', permissions: [] });
      throw error;
    }
  }
  can(permission: string): boolean { return this.identity().permissions.includes(permission); }
}
