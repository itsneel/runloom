export interface OrganizationSummary {
  id: string;
  name: string;
  parent_id?: string;
  role?: string;
}
export interface Hierarchy {
  root: OrganizationSummary;
  children: OrganizationSummary[];
  max_children: number;
}
interface CreationOptions {
  can_create: boolean;
  can_create_root: boolean;
  max_children: number;
  parents: OrganizationSummary[];
}
interface OrganizationContext {
  organization(): OrganizationSummary | undefined;
  request<T>(path: string, body?: unknown, method?: string): Promise<T>;
  refreshProfile(): Promise<void>;
  selectOrganization(id: string): Promise<void>;
  confirm(message: string): Promise<boolean>;
  markForCheck(): void;
}
export class OrganizationWorkspace {
  options?: CreationOptions;
  hierarchy?: Hierarchy;
  name = '';
  parent = '';
  rename = '';
  private baseline = '';
  private generation = 0;
  busy = false;
  loading = false;
  error = '';
  notice = '';
  confirmedExit = false;
  constructor(private readonly context: OrganizationContext) {}
  get dirty(): boolean {
    return this.busy || !!this.name.trim() || this.rename !== this.baseline;
  }
  get current(): OrganizationSummary | undefined {
    return this.context.organization();
  }
  async load(): Promise<void> {
    const generation = ++this.generation;
    this.busy = false;
    this.name = '';
    this.parent = '';
    this.rename = this.baseline = this.current?.name || '';
    this.options = undefined;
    this.hierarchy = undefined;
    this.error = '';
    this.notice = '';
    this.loading = true;
    try {
      const id = this.current?.id;
      const [options, hierarchy] = await Promise.all([
        this.context.request<CreationOptions>(
          '/api/v1/studio/orgs/creation' + (id ? `?organization_id=${encodeURIComponent(id)}` : ''),
        ),
        id
          ? this.context.request<Hierarchy>(`/api/v1/studio/orgs/${id}/hierarchy`)
          : Promise.resolve(undefined),
      ]);
      if (generation !== this.generation) return;
      this.options = options;
      this.parent = options.can_create_root ? '' : options.parents[0]?.id || '';
      this.hierarchy = hierarchy;
    } catch (error) {
      if (generation === this.generation) this.error = (error as Error).message;
    } finally {
      if (generation === this.generation) this.loading = false;
      this.context.markForCheck();
    }
  }
  destroy(): void {
    this.generation++;
  }
  async confirmDiscard(): Promise<boolean> {
    if (this.busy) return false;
    if (!this.dirty) return true;
    const generation = this.generation,
      snapshot = JSON.stringify([this.name, this.rename]);
    return (
      (await this.context.confirm('Discard unsaved organization changes?')) &&
      !this.busy &&
      generation === this.generation &&
      snapshot === JSON.stringify([this.name, this.rename])
    );
  }
  async switchTo(org: OrganizationSummary): Promise<void> {
    if (!org.role || !(await this.confirmDiscard())) return;
    this.name = '';
    this.rename = this.baseline;
    await this.context.selectOrganization(org.id);
  }
  async create(): Promise<void> {
    if (this.busy || !this.options?.can_create) return;
    if (!this.parent && !this.options.can_create_root) {
      this.error = 'You can only create a child within your organization family.';
      return;
    }
    if (this.parent && !this.options.parents.some((parent) => parent.id === this.parent)) {
      this.error = 'Select a parent organization you administer.';
      return;
    }
    if (!this.name.trim()) {
      this.error = 'Enter an organization name.';
      return;
    }
    const generation = this.generation;
    this.busy = true;
    this.error = '';
    this.notice = '';
    try {
      const created = await this.context.request<{ id: string }>('/api/v1/studio/orgs', {
        name: this.name.trim(),
        parent_id: this.parent,
      });
      if (generation !== this.generation) return;
      this.name = '';
      this.parent = '';
      await this.context.refreshProfile();
      if (generation !== this.generation) return;
      this.busy = false;
      await this.context.selectOrganization(created.id);
    } catch (error) {
      if (generation === this.generation) this.error = (error as Error).message;
    } finally {
      if (generation === this.generation) this.busy = false;
      this.context.markForCheck();
    }
  }
  async saveName(): Promise<void> {
    if (this.busy || !this.current || !['owner', 'admin'].includes(this.current.role || '')) return;
    const generation = this.generation,
      id = this.current.id;
    this.busy = true;
    this.error = '';
    this.notice = '';
    try {
      await this.context.request(
        `/api/v1/studio/orgs/${id}`,
        { name: this.rename.trim() },
        'PATCH',
      );
      if (generation !== this.generation) return;
      this.rename = this.baseline = this.rename.trim();
      await this.context.refreshProfile();
      if (generation !== this.generation) return;
      this.notice = 'Organization renamed. Its ID and Temporal namespace are unchanged.';
      this.busy = false;
      await this.loadHierarchy();
    } catch (error) {
      if (generation === this.generation) this.error = (error as Error).message;
    } finally {
      if (generation === this.generation) this.busy = false;
      this.context.markForCheck();
    }
  }
  private async loadHierarchy(): Promise<void> {
    const generation = this.generation,
      id = this.current?.id;
    if (!id) return;
    const hierarchy = await this.context.request<Hierarchy>(`/api/v1/studio/orgs/${id}/hierarchy`);
    if (generation === this.generation) this.hierarchy = hierarchy;
  }
}
