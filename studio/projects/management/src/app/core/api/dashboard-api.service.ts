import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { firstValueFrom } from 'rxjs';

@Injectable({ providedIn: 'root' })
export class DashboardApi {
  private readonly http = inject(HttpClient);
  get<T>(team: string, path: string): Promise<T> {
    return firstValueFrom(
      this.http.get<T>('/api/v1/studio/' + path, { headers: { 'X-Runloom-Team': team } }),
    );
  }
  post<T>(team: string, path: string, body: unknown): Promise<T> {
    return firstValueFrom(
      this.http.post<T>('/api/v1/studio/' + path, body, { headers: { 'X-Runloom-Team': team } }),
    );
  }
  delete(team: string, path: string): Promise<unknown> {
    return firstValueFrom(
      this.http.delete('/api/v1/studio/' + path, { headers: { 'X-Runloom-Team': team } }),
    );
  }
}
