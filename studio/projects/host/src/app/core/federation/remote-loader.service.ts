import { Injectable, Type } from '@angular/core';
import { loadRemoteModule } from '@angular-architects/module-federation';
import { ApplicationKind } from '@runloom/studio-platform';
export type HostedApplication = ApplicationKind | 'platform';

interface RemoteApplication { contractVersion: number; Application: Type<unknown>; }

@Injectable({providedIn:'root'})
export class RemoteLoader {
  private readonly loaded = new Map<HostedApplication, Promise<Type<unknown>>>();
  private attempts = 0;
  load(kind: HostedApplication): Promise<Type<unknown>> {
    const cached = this.loaded.get(kind);
    if (cached) return cached;
    const result = this.fetch(kind).catch(error => { this.loaded.delete(kind); throw error; });
    this.loaded.set(kind, result);
    return result;
  }
  private async fetch(kind: HostedApplication): Promise<Type<unknown>> {
    const response = await fetch('/federation.manifest.json', {cache:'no-cache',signal:AbortSignal.timeout(15000)});
    if (!response.ok) throw new Error('Application manifest unavailable');
    const manifest = await response.json();
    if (typeof manifest[kind] !== 'string') throw new Error('Application not configured');
    const url = new URL(manifest[kind], window.location.origin);
    // Remote JavaScript has host privileges. Only operator-controlled, same-origin assets are accepted.
    if (url.origin !== window.location.origin || !url.pathname.startsWith(`/remotes/${kind}/`) || url.username || url.password || url.hash) {
      throw new Error('Untrusted application location');
    }
    url.searchParams.set('load', String(++this.attempts));
    const remote = await loadRemoteModule<RemoteApplication>({type:'module',remoteEntry:url.href,exposedModule:'./Application'});
    if (remote.contractVersion !== 1 || !remote.Application) throw new Error('Incompatible application contract');
    return remote.Application;
  }
}
