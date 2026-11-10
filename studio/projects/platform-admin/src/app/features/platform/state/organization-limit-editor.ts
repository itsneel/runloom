import { OrganizationChildPolicy, OrganizationMetadata } from '../services/platform-api';

interface OrganizationLimitContext {
  canEdit(): boolean;
  save(id: string, limit?: number): Promise<OrganizationChildPolicy>;
  saved(): void;
}
export class OrganizationLimitEditor {
  organization?: OrganizationMetadata;
  editing = false;
  busy = false;
  error = '';
  mode = 'default';
  limit = 2;
  private generation = 0;
  constructor(private readonly context: OrganizationLimitContext) {}
  load(org: OrganizationMetadata): void {
    this.generation++;
    this.organization = org;
    this.editing = this.busy = false;
    this.error = '';
    this.mode = org.child_limit?.override == null ? 'default' : 'custom';
    this.limit = org.child_limit?.max_children ?? 2;
  }
  destroy(): void {
    this.generation++;
  }
  edit(): void {
    if (!this.context.canEdit() || !this.organization?.child_limit || this.busy) return;
    this.editing = true;
    this.error = '';
  }
  cancel(): void {
    if (!this.busy && this.organization) this.load(this.organization);
  }
  async save(): Promise<void> {
    const org = this.organization;
    if (this.busy || !this.editing || !this.context.canEdit() || !org?.child_limit) return;
    if (
      !['default', 'custom'].includes(this.mode) ||
      (this.mode === 'custom' &&
        (!Number.isInteger(this.limit) || this.limit < 0 || this.limit > 10))
    ) {
      this.error = 'Choose a whole number from 0 to 10.';
      return;
    }
    const generation = this.generation;
    this.busy = true;
    this.error = '';
    try {
      const policy = await this.context.save(
        org.id,
        this.mode === 'custom' ? this.limit : undefined,
      );
      if (generation !== this.generation) return;
      this.load({ ...org, child_limit: policy });
      this.context.saved();
    } catch (error) {
      if (generation === this.generation) this.error = (error as Error).message;
    } finally {
      if (generation === this.generation) this.busy = false;
    }
  }
}
