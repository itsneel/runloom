import { computed, Injectable, signal } from '@angular/core';
import Keycloak from 'keycloak-js';
import { authReturnUrl } from './auth-navigation';

// Workspace preferences are optional; unavailable storage must not block rendering.
function readPreference(key: string): string {
  try { return localStorage.getItem(key) || ''; } catch { return ''; }
}
function storePreference(key: string, value: string): void {
  try { localStorage.setItem(key, value); } catch {}
}

export interface Team {
  id: string;
  name: string;
  role: 'admin' | 'editor' | 'viewer';
}
export interface Organization {
  id: string;
  name: string;
  role: 'owner' | 'admin' | 'member';
  teams: Team[];
}
export interface Member {
  email: string;
  user_id: string;
  role: string;
}
interface AuthConfig {
  url: string;
  realm: string;
  clientId: string;
  googleEnabled: boolean;
}
interface Profile {
  user: { id: string; email: string; name: string };
  organizations: Organization[];
}

@Injectable({ providedIn: 'root' })
export class AuthService {
  private keycloak?: Keycloak;
  config?: AuthConfig;
  readonly ready = signal(false);
  readonly signedIn = signal(false);
  readonly profile = signal<Profile | undefined>(undefined);
  readonly organizationId = signal(readPreference('runloom-org'));
  readonly teamId = signal(readPreference('runloom-team'));
  readonly organizations = computed(() => this.profile()?.organizations || []);
  readonly organization = computed(() =>
    this.organizations().find((o) => o.id === this.organizationId()),
  );
  readonly team = computed(() => this.organization()?.teams.find((t) => t.id === this.teamId()));
  readonly canEdit = computed(() => !!this.team() && this.team()?.role !== 'viewer');
  readonly orgAdmin = computed(() => ['owner', 'admin'].includes(this.organization()?.role || ''));
  readonly teamAdmin = computed(() => this.team()?.role === 'admin');

  async initialize(): Promise<void> {
    const response = await fetch('/api/v1/auth/config');
    if (!response.ok) throw new Error('Sign-in service is unavailable.');
    this.config = (await response.json()) as AuthConfig;
    this.keycloak = new Keycloak(this.config);
    const authenticated = await this.keycloak.init({
      onLoad: 'login-required',
      pkceMethod: 'S256',
      checkLoginIframe: false,
      responseMode: 'query',
      redirectUri: authReturnUrl(window.location.href),
    });
    if (authenticated) await this.refresh();
    this.signedIn.set(authenticated);
    this.ready.set(true);
  }

  async bearer(): Promise<string> {
    if (!this.keycloak?.authenticated) throw new Error('Please sign in.');
    try {
      await this.keycloak.updateToken(30);
      return this.keycloak.token!;
    } catch {
      this.signedIn.set(false);
      this.profile.set(undefined);
      throw new Error('Your session expired. Please sign in again.');
    }
  }

  async request<T>(path: string, body?: unknown, method?: string): Promise<T> {
    const response = await fetch(path, {
      method: method || (body === undefined ? 'GET' : 'POST'),
      headers: {
        Authorization: 'Bearer ' + (await this.bearer()),
        'Content-Type': 'application/json',
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (!response.ok) {
      const message = await response.json().catch(() => ({}));
      throw new Error(
        message.detail || message.error || 'Request failed (' + response.status + ')',
      );
    }
    return response.json() as Promise<T>;
  }

  async refresh(): Promise<void> {
    this.profile.set(await this.request<Profile>('/api/v1/studio/me'));
    if (!this.organization()) this.selectOrganization(this.organizations()[0]?.id || '');
    else if (!this.team()) this.selectTeam(this.organization()?.teams[0]?.id || '');
  }

  selectOrganization(id: string): void {
    this.organizationId.set(id);
    storePreference('runloom-org', id);
    this.selectTeam(this.organization()?.teams[0]?.id || '');
  }
  selectTeam(id: string): void {
    this.teamId.set(id);
    storePreference('runloom-team', id);
  }
  login(google = false): Promise<void> {
    return this.keycloak!.login({
      redirectUri: authReturnUrl(window.location.href),
      ...(google ? { idpHint: 'google' } : {}),
    });
  }
  register(): Promise<void> {
    return this.keycloak!.register({ redirectUri: authReturnUrl(window.location.href) });
  }
  logout(): Promise<void> {
    return this.keycloak!.logout({ redirectUri: window.location.origin + '/' });
  }
  forgotPassword(): void {
    const c = this.config!;
    const url = new URL(
      c.url.replace(/\/$/, '') +
        '/realms/' +
        encodeURIComponent(c.realm) +
        '/login-actions/reset-credentials',
      window.location.origin,
    );
    url.searchParams.set('client_id', c.clientId);
    window.location.assign(url.toString());
  }
}
