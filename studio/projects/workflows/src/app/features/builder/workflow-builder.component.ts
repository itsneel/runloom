import { SignalHandlers } from './components/signal-handlers/signal-handlers.component';
import type { AuthoringDraftRequest } from '@runloom/studio-platform';
import { WorkflowHistory, isTextEditingTarget } from './state/workflow-history';
import { WorkflowAutosave } from './state/workflow-autosave';
import { WorkflowVersions } from './state/workflow-versions';
import { WorkflowEditor } from './state/workflow-editor';
import { WorkflowEditLock } from './state/workflow-edit-lock';
import { WorkflowTestRun } from './state/workflow-test-run';
import { LibraryDeletion } from './state/library-deletion';
import { emptyDefinition, exampleDefinition } from './model/workflow-defaults';
import { WorkflowExamples } from './state/workflow-examples';
import {
  workflowPalette,
  stepLabel,
  stepIcon,
  stepTone,
  stepSummary,
} from './model/node-presentation';
import { WorkspaceBridge, injectDialogs } from '@runloom/studio-platform';
import { CommonModule } from '@angular/common';
import {
  ChangeDetectorRef,
  Component,
  ElementRef,
  EventEmitter,
  HostListener,
  inject,
  Input,
  OnDestroy,
  OnInit,
  Output,
  TemplateRef,
  ViewChild,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router } from '@angular/router';
import { definitionUrl, projectUrl } from '../../core/routing/workflow-paths';
import { CodeEditor } from './components/code-editor/code-editor.component';
import { DatabaseEditor } from './components/database-editor/database-editor.component';
import { DataEditor } from './components/data-editor/data-editor.component';
import { LLMTaskEditor } from './components/llm-task-editor/llm-task-editor.component';
import { IntegrationEditor } from './components/integration-editor/integration-editor.component';
import { PromptEditor } from './components/prompt-editor/prompt-editor.component';
import { WorkflowDashboard } from '../dashboard/workflow-dashboard.component';
import { ProjectsDashboard } from '../projects/projects-dashboard.component';
import { WorkflowGraph } from './components/canvas/workflow-graph.component';
import { allSteps, allDefinitionSteps, rootPaths, containsPath } from './model/workflow-tree';
import { RunCosts } from '../runs/components/run-costs/run-costs.component';
import { Notice } from '@runloom/studio-platform';
import { UiIcon } from '@runloom/studio-platform';
import { AuthService } from '@runloom/studio-platform';
import { EditLockStatus, WorkflowApiService } from '../../core/api/workflow-api.service';
import {
  DefinitionSummary,
  WorkflowDefinition,
  WorkflowProject,
} from '../../core/models/workflow.types';

@Component({
  imports: [
    SignalHandlers,
    CommonModule,
    FormsModule,
    CodeEditor,
    DatabaseEditor,
    DataEditor,
    LLMTaskEditor,
    IntegrationEditor,
    WorkflowGraph,
    RunCosts,
    Notice,
    UiIcon,
    PromptEditor,
    WorkflowDashboard,
    ProjectsDashboard,
  ],
  providers: [WorkflowApiService],
  selector: 'runloom-studio',
  styleUrl: './workflow-builder.component.scss',
  templateUrl: './workflow-builder.component.html',
})
export class App implements OnInit, OnDestroy {
  @ViewChild('workflowNavigation', { static: true }) workflowNavigation!: TemplateRef<unknown>;
  @Output() toggleNavigation = new EventEmitter<void>();
  @Input() navigationExpanded = true;
  @Input() showNavigationToggle = false;
  @Input() projectsView = false;
  @Input() routeProjectId = '';
  @Input() routeDefinitionId = '';
  private readonly router = inject(Router, { optional: true });
  routeLoading = false;
  routeError = '';
  projectOpened = false;
  projectsLoading = true;
  projectsError = '';
  private readonly dialogs = injectDialogs();
  readonly auth = inject(AuthService);
  private readonly api = inject(WorkflowApiService);
  private readonly changeDetector = inject(ChangeDetectorRef);
  private destroyed = false;

  get isNewWorkflow(): boolean {
    return !this.definitions.some((d) => d.id === this.definition.id);
  }

  @HostListener('window:focus')
  refreshEditingOnFocus(): void {
    void this.edit.refreshEditLock();
  }

  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);

  @HostListener('document:keydown', ['$event'])
  onBuilderKeydown(event: KeyboardEvent): void {
    if (event.ctrlKey || event.metaKey) {
      this.saveShortcut(event);
      this.historyShortcut(event);
    } else {
      void this.canvasShortcut(event);
    }
  }

  async canvasShortcut(event: KeyboardEvent): Promise<void> {
    if (!this.builderOpen) return;
    const target = event.composedPath?.()[0] ?? event.target;
    if (target instanceof Node && target !== document.body && target !== document.documentElement &&
        !this.host.nativeElement.contains(target)) return;
    if (
      event.defaultPrevented ||
      event.ctrlKey ||
      event.metaKey ||
      event.altKey ||
      event.repeat ||
      event.isComposing ||
      this.jsonOpen ||
      this.versions.versionOpen ||
      this.testRun.runOpen ||
      document.querySelector('dialog[open]') ||
      isTextEditingTarget(event.composedPath?.()[0] ?? event.target)
    )
      return;

    const key =
      event.key === '?'
        ? '?'
        : event.shiftKey
          ? event.key.toLowerCase() === 'p'
            ? 'Shift+P'
            : ''
          : event.key.toUpperCase();
    if (!['E', 'Shift+P', 'A', 'V', 'T', 'H', '?'].includes(key)) return;
    const root = this.host.nativeElement;
    const button = root.querySelector<HTMLButtonElement>(`button[aria-keyshortcuts="${key}"]`);
    if (!button || button.disabled) return;
    event.preventDefault();

    if (key === 'Shift+P') {
      const generation = this.documentGeneration;
      const snapshot = this.editSnapshot();
      const confirmed = await this.dialogs.confirm(
        `Publish “${this.definition.name}” as a new version?`,
        { title: 'Publish workflow?', confirmText: 'Publish' },
      );
      if (
        !confirmed ||
        this.destroyed ||
        generation !== this.documentGeneration ||
        snapshot !== this.editSnapshot() ||
        !button.isConnected ||
        button.disabled ||
        this.versions.saving ||
        this.projectBusy ||
        this.draftValidationError ||
        !this.edit.canEditWorkflow
      )
        return;
    }
    if (key === 'V') {
      const target = document.activeElement;
      const generation = this.documentGeneration;
      if (
        !(await this.validate()) &&
        !this.destroyed &&
        generation === this.documentGeneration &&
        target === document.activeElement
      ) {
        this.changeDetector.detectChanges();
        const issue =
          root.querySelector<HTMLElement>('.node-select[aria-invalid="true"]') ??
          root.querySelector<HTMLElement>('.validation-summary, .builder-notice');
        issue?.focus();
      }
      return;
    }
    button.click();
    const focusSelector =
      key === 'A'
        ? '.picker-search input'
        : key === 'T'
          ? '.run-drawer.open:not(.version-drawer) .field :is(input, select, textarea)'
          : key === 'H'
            ? '.version-drawer .drawer-head button'
            : '';
    if (focusSelector) {
      this.changeDetector.detectChanges();
      root.querySelector<HTMLElement>(focusSelector)?.focus();
    }
  }

  saveShortcut(event: KeyboardEvent): void {
    if (
      !(event.metaKey || event.ctrlKey) ||
      event.altKey ||
      event.shiftKey ||
      event.key.toLowerCase() !== 's'
    )
      return;
    event.preventDefault();
    if (
      event.repeat ||
      event.isComposing ||
      !this.builderOpen ||
      !this.edit.canEditWorkflow ||
      document.querySelector('dialog[open]') ||
      this.jsonOpen ||
      this.testRun.runOpen ||
      this.versions.versionOpen
    )
      return;
    void this.versions.save();
  }

  historyShortcut(event: KeyboardEvent): void {
    if (
      event.defaultPrevented ||
      !(event.metaKey || event.ctrlKey) ||
      event.altKey ||
      event.key.toLowerCase() !== 'z' ||
      event.repeat ||
      event.isComposing ||
      !this.canUseHistory ||
      document.querySelector('dialog[open]') ||
      isTextEditingTarget(event.composedPath?.()[0] ?? event.target)
    )
      return;
    event.preventDefault();
    this.restoreHistory(event.shiftKey);
  }

  get canUseHistory(): boolean {
    return (
      this.builderOpen &&
      this.edit.canEditWorkflow &&
      !this.versions.saving &&
      !this.projectBusy &&
      !this.openingDefinitionId &&
      !this.jsonOpen &&
      !this.testRun.runOpen &&
      !this.versions.versionOpen &&
      !this.editor.stepDataDrafts.size &&
      !this.editor.schemaDrafts.size
    );
  }
  restoreHistory(redo = false): void {
    if (!this.canUseHistory) return;
    const snapshot = redo ? this.history.redo() : this.history.undo();
    if (!snapshot) return;
    this.definition = JSON.parse(snapshot) as WorkflowDefinition;
    this.editor.selectedContainer = undefined;
    this.editor.selectedIndex = -1;
    this.editor.nodeInsertSteps = undefined;
    this.editor.nodePickerOpen = false;
    this.draftValidationError = '';
    this.nodeValidationErrors = Object.create(null);
    this.syncJson();
    this.autosave.changed();
    this.changeDetector.markForCheck();
  }

  get saveStatus(): string {
    if (this.versions.saving) return 'Saving…';
    if (!this.edit.canEditWorkflow)
      return this.hasUnsavedChanges ? 'Unsaved changes · editing paused' : 'Read-only';
    if (this.isNewWorkflow) return 'Save once to enable autosave';
    if (!this.hasUnsavedChanges) return 'Saved';
    if (this.autosave.failed || this.versions.saveFailed)
      return 'Save failed · changes kept locally';
    if (this.hasUnappliedJson || this.editor.stepDataDrafts.size || this.editor.schemaDrafts.size)
      return 'Autosave paused · finish JSON edits';
    return 'Unsaved changes';
  }

  definition: WorkflowDefinition = emptyDefinition();
  private savedSnapshot = this.definitionSnapshot();
  readonly history = new WorkflowHistory(this.savedSnapshot);
  readonly autosave: WorkflowAutosave;
  private documentGeneration = 0;
  private jsonEditorSnapshot = '';
  private confirmedExitSnapshot?: string;
  definitions: DefinitionSummary[] = [];
  builderOpen = false;
  get assistantContext():
    { workflowId: string; name: string; saved: boolean; canApply: boolean } | undefined {
    return this.builderOpen
      ? {
          workflowId: this.definition.id,
          name: this.definition.name,
          saved: !this.isNewWorkflow,
          canApply:
            this.auth.canEdit() &&
            !this.hasUnsavedChanges &&
            !this.edit.editingInterrupted &&
            !this.edit.acquiringEdit &&
            !this.versions.saving &&
            !this.openingDefinitionId &&
            !this.projectBusy,
        }
      : undefined;
  }
  async prepareAuthoringDraft(): Promise<void> {
    const id = this.definition.id,
      generation = this.documentGeneration;
    if (!this.builderOpen || this.isNewWorkflow)
      throw new Error('Save this workflow before asking Lumi to edit it.');
    for (
      let attempt = 0;
      this.versions.saving ||
      this.edit.acquiringEdit ||
      this.openingDefinitionId ||
      this.projectBusy;
      attempt++
    ) {
      if (this.destroyed || generation !== this.documentGeneration || id !== this.definition.id)
        throw new Error('The selected workflow changed while preparing Lumi.');
      if (attempt >= 200)
        throw new Error(
          'The workflow is still saving or loading. Your message is kept; retry when it finishes.',
        );
      await new Promise<void>((resolve) => setTimeout(resolve, 50));
    }
    if (this.edit.editingInterrupted)
      throw new Error(
        'Your editing session expired. Your local changes are kept; reload before asking Lumi to edit.',
      );
    if (this.hasUnsavedChanges && !(await this.versions.save(true)))
      throw new Error(
        'Save your pending canvas or JSON edits before asking Lumi to edit. Your changes were kept.',
      );
    if (
      this.destroyed ||
      generation !== this.documentGeneration ||
      id !== this.definition.id ||
      this.hasUnsavedChanges
    )
      throw new Error(
        'The workflow changed while preparing Lumi. Retry from the selected saved draft.',
      );
  }
  async applyAuthoringDraft(request: AuthoringDraftRequest): Promise<void> {
    if (
      request.workflowId !== this.definition.id ||
      request.orgId !== this.auth.organizationId() ||
      request.teamId !== this.auth.teamId() ||
      !this.assistantContext?.canApply
    )
      throw new Error(
        'The selected workflow has changed or has unsaved edits. Save them before retrying Lumi.',
      );
    await this.api.applyAuthoringDraft(request);
  }
  async refreshAuthoringDraft(workflowId: string): Promise<void> {
    if (!this.builderOpen) {
      await this.loadDefinitions();
      return;
    }
    if (this.definition.id !== workflowId) return;
    if (!this.assistantContext?.canApply) {
      this.notify(
        'Assistant changes were saved. Finish your current edits and reopen this workflow to load them.',
      );
      return;
    }
    const resumeEditing = this.edit.ownsEdit;
    await this.loadDefinitions();
    if (this.definition.id === workflowId && this.assistantContext?.canApply) {
      await this.openDefinition(workflowId);
      if (resumeEditing && this.definition.id === workflowId && !this.hasUnsavedChanges)
        await this.edit.editWorkflow();
    }
  }
  definitionsLoading = true;
  definitionsError = '';
  openingDefinitionId = '';
  projects: WorkflowProject[] = [];
  activeProjectId = 'default';
  workflowProjectId = 'default';
  private savedProjectId = 'default';
  projectBusy = false;
  workflowSearch = '';
  rulesOpen = true;
  jsonOpen = false;
  connected = false;
  message = '';
  messageError = false;
  messageRevision = 0;
  databaseDriver = 'file';
  jsonDraft = '';

  async ngOnInit(): Promise<void> {
    this.routeLoading = !!(this.routeProjectId || this.routeDefinitionId);
    this.edit.start();
    this.syncJson();
    await Promise.all([this.refreshHealth(), this.loadDefinitions(), this.loadProjects()]);
    if (this.destroyed) return;
    if (this.routeLoading) {
      if (this.projectsError || this.definitionsError)
        this.routeError = this.projectsError || this.definitionsError;
      else if (this.routeDefinitionId)
        await this.openDefinition(this.routeDefinitionId, true, true);
      else if (!this.projects.some((project) => project.id === this.routeProjectId))
        this.routeError = 'This project does not exist or is not available in the selected team.';
      else await this.selectProject(this.routeProjectId, true);
    }
    this.routeLoading = false;
    const example =
      typeof window !== 'undefined' ? window.history.state?.runloomExample : undefined;
    if (
      this.builderOpen &&
      example?.workflowId === this.definition.id &&
      example.input &&
      typeof example.input === 'object' &&
      !Array.isArray(example.input)
    ) {
      this.testRun.runInput = JSON.stringify(example.input, null, 2);
      this.notify('Parallel example ready. Test this draft to run both child workflows together.');
    }
    this.changeDetector.markForCheck();
  }

  ngOnDestroy(): void {
    this.destroyed = true;
    clearTimeout(this.validationTimer);
    this.autosave.dispose();
    this.edit.dispose();
    this.testRun.stopPolling();
  }

  get hasUnsavedChanges(): boolean {
    return (
      this.workflowProjectId !== this.savedProjectId ||
      this.definitionSnapshot() !== this.savedSnapshot ||
      this.hasUnappliedJson ||
      this.editor.stepDataDrafts.size > 0 ||
      this.editor.schemaDrafts.size > 0
    );
  }

  private get hasUnappliedJson(): boolean {
    return this.jsonOpen && this.jsonDraft !== this.jsonEditorSnapshot;
  }

  async confirmDiscardChanges(): Promise<boolean> {
    if (this.projectBusy || this.versions.saving || this.edit.acquiringEdit) return false;
    if (!this.hasUnsavedChanges) return true;
    const snapshot = this.editSnapshot();
    const generation = this.documentGeneration;
    const accepted = await this.dialogs.confirm(
      'You have unsaved workflow changes. Discard them and continue?',
      {
        title: 'Discard unsaved changes?',
        confirmText: 'Discard changes',
        cancelText: 'Keep editing',
        danger: true,
      },
    );
    return (
      accepted &&
      !this.destroyed &&
      generation === this.documentGeneration &&
      snapshot === this.editSnapshot()
    );
  }

  @HostListener('window:beforeunload', ['$event'])
  beforeUnload(event: BeforeUnloadEvent): void {
    if (this.confirmedExitSnapshot === this.editSnapshot()) {
      this.confirmedExitSnapshot = undefined;
      return;
    }
    if (!this.hasUnsavedChanges) return;
    event.preventDefault();
    event.returnValue = '';
  }

  async withConfirmedExit(work: () => Promise<void>): Promise<void> {
    // Sign-out has already been confirmed. Avoid a second native dialog, but
    // protect any edits made after that confirmation and re-arm on failure.
    this.confirmedExitSnapshot = this.editSnapshot();
    try {
      await this.edit.releaseEditing();
      await work();
    } finally {
      this.confirmedExitSnapshot = undefined;
    }
  }

  private definitionSnapshot(definition = this.definition): string {
    // Publishing changes the version, not the editable workflow content.
    return JSON.stringify({ ...definition, version: 0 }, (_key, value) =>
      value?.type === 'condition'
        ? { ...value, then: value.then ?? [], else: value.else ?? [] }
        : value,
    );
  }

  private replaceDefinition(
    definition: WorkflowDefinition,
    saved = true,
    projectId = this.workflowProjectId,
    preserveHistory = false,
  ): void {
    this.documentGeneration++;
    this.autosave.reset();
    this.versions.saveFailed = false;
    this.draftValidationError = '';
    this.nodeValidationErrors = Object.create(null);
    this.definition = definition;
    if (!preserveHistory) this.history.reset(this.definitionSnapshot());
    this.editor.selectedContainer = undefined;
    this.editor.nodeInsertSteps = undefined;
    this.editor.nodePickerOpen = false;
    this.workflowProjectId = projectId;
    this.editor.stepDataDrafts.clear();
    this.editor.schemaDrafts.clear();
    this.jsonOpen = false;
    this.editor.selectedIndex = definition.steps.length ? 0 : -1;
    if (saved) {
      this.savedSnapshot = this.definitionSnapshot();
      this.savedProjectId = projectId;
    }
    this.syncJson();
  }

  private editSnapshot(): string {
    return JSON.stringify([
      this.definitionSnapshot(),
      this.workflowProjectId,
      this.jsonOpen ? this.jsonDraft : null,
      [...this.editor.stepDataDrafts.values()],
      [...this.editor.schemaDrafts.values()],
    ]);
  }

  private canApplyResponse(generation: number, snapshot: string): boolean {
    if (this.destroyed || generation !== this.documentGeneration) return false;
    if (snapshot === this.editSnapshot()) return true;
    this.notify('The workflow changed while loading. Your newer edits were kept; try again.', true);
    return false;
  }

  get publishLabel(): string {
    return this.definition.version > 0 ? `Published v${this.definition.version}` : 'Draft';
  }

  get webhookUrl(): string {
    if (this.definition.trigger.type !== 'webhook') return '';
    return `${window.location.origin}/api/v1/studio/webhooks/${this.definition.id}/${this.definition.trigger.path}`;
  }

  async newDefinition(): Promise<void> {
    if (
      !this.auth.canEdit() ||
      this.projectBusy ||
      this.versions.saving ||
      this.openingDefinitionId
    )
      return;
    if (!(await this.confirmDiscardChanges())) return;
    void this.edit.releaseEditing();
    this.edit.editLost = false;
    this.edit.editLock = undefined;
    this.replaceDefinition(emptyDefinition(), true, this.activeProjectId);
    this.editor.selectedIndex = -1;
    this.testRun.run = undefined;
    this.versions.definitionHistory = undefined;
    this.resetDetailPanels();
    this.builderOpen = true;
    this.syncJson();
    this.notify('Started a clean workflow draft.');
  }

  async loadExample(kind: 'review' | 'parallel-subworkflows' = 'review'): Promise<void> {
    if (
      !this.auth.canEdit() ||
      this.projectBusy ||
      this.versions.saving ||
      this.openingDefinitionId
    )
      return;
    if (!(await this.confirmDiscardChanges())) return;
    if (kind === 'parallel-subworkflows') {
      await this.examples.createParallel();
      return;
    }
    void this.edit.releaseEditing();
    this.edit.editLost = false;
    this.edit.editLock = undefined;
    const example = exampleDefinition();
    example.id = `example-${crypto.randomUUID()}`;
    this.replaceDefinition(example, false, this.activeProjectId);
    this.editor.selectedIndex = 0;
    this.resetDetailPanels();
    this.builderOpen = true;
    this.syncJson();
    this.notify('Example loaded. It includes every kind of stop and restart.');
  }

  async openDefinition(id: string, discardConfirmed = false, fromRoute = false): Promise<void> {
    if (this.projectBusy || this.versions.saving || this.openingDefinitionId) return;
    if (!fromRoute && this.router && this.routeDefinitionId !== id) {
      await this.router.navigateByUrl(definitionUrl(id));
      return;
    }
    if (!discardConfirmed && !(await this.confirmDiscardChanges())) return;
    this.openingDefinitionId = id;
    const generation = ++this.documentGeneration;
    const snapshot = this.editSnapshot();
    await this.edit.releaseEditing();
    this.edit.editLost = false;
    this.edit.editLock = undefined;
    try {
      if (this.destroyed || generation !== this.documentGeneration) return;
      const definition = await this.api
        .getDefinition(id, 0)
        .catch(() => this.api.getDefinition(id));
      if (!this.canApplyResponse(generation, snapshot)) return;
      const projectId = this.definitions.find((item) => item.id === id)?.project_id ?? 'default';
      this.replaceDefinition(definition, true, projectId);
      this.activeProjectId = projectId;
      this.editor.selectedIndex = this.definition.steps.length ? 0 : -1;
      this.versions.definitionHistory = undefined;
      this.resetDetailPanels();
      this.builderOpen = true;
      this.syncJson();
      this.message = '';
      await this.edit.refreshEditLock();
    } catch (error) {
      if (fromRoute) this.routeError = this.errorText(error);
      this.handleError(error);
    } finally {
      this.openingDefinitionId = '';
      this.changeDetector.markForCheck();
    }
  }

  private resetDetailPanels(): void {
    this.testRun.stopPolling();
    this.testRun.run = undefined;
    this.testRun.runHistory = undefined;
    this.testRun.runOpen = false;
    this.versions.versionOpen = false;
    this.editor.nodePickerOpen = false;
  }

  async showDashboard(resetProjects = true): Promise<boolean> {
    if (this.router && this.routeDefinitionId)
      return this.router.navigateByUrl(projectUrl(this.activeProjectId));
    if (
      this.projectBusy ||
      this.versions.saving ||
      this.edit.acquiringEdit ||
      this.openingDefinitionId ||
      !(await this.confirmDiscardChanges())
    )
      return false;
    await this.edit.releaseEditing();
    this.edit.editLost = false;
    this.edit.editLock = undefined;
    this.replaceDefinition(emptyDefinition(), true, this.activeProjectId);
    this.resetDetailPanels();
    this.versions.definitionHistory = undefined;
    this.builderOpen = false;
    if (resetProjects) this.projectOpened = false;
    this.message = '';
    await this.loadDefinitions();
    this.changeDetector.markForCheck();
    return true;
  }

  markChanged(): void {
    this.draftValidationError = '';
    this.definition.version = 0;
    this.syncJson();
    const target = document.activeElement;
    this.history.record(
      this.definitionSnapshot(),
      isTextEditingTarget(target) ? target! : undefined,
    );
    this.autosave.changed();
  }

  draftValidationError = '';
  private nodeValidationErrors: Record<string, string> = Object.create(null);
  private validationTimer?: ReturnType<typeof setTimeout>;
  private validationRequest = 0;
  get nodeErrors(): Record<string, string> {
    const errors: Record<string, string> = Object.assign(
      Object.create(null),
      this.nodeValidationErrors,
    );
    for (const step of this.editor.stepDataDrafts.keys())
      errors[step.id] = 'Fix the invalid JSON in this node’s configuration.';
    for (const target of this.editor.schemaDrafts.keys())
      if (!('steps' in target)) errors[target.id] = 'Output schema must be a valid JSON object.';
    return errors;
  }
  private recordNodeErrors(error?: unknown): void {
    if (error && (error as { status?: number }).status !== 422) return;
    const issues = (error as { error?: { issues?: { node_id: string; message: string }[] } })?.error
      ?.issues;
    this.nodeValidationErrors = Object.create(null);
    for (const issue of issues ?? []) {
      const previous = this.nodeValidationErrors[issue.node_id];
      this.nodeValidationErrors[issue.node_id] = previous
        ? `${previous}\n${issue.message}`
        : issue.message;
    }
  }
  private scheduleValidation(): void {
    clearTimeout(this.validationTimer);
    if (this.destroyed || !this.builderOpen || !this.auth.canEdit()) return;
    this.validationTimer = setTimeout(() => {
      if (!this.builderOpen) return;
      const generation = this.documentGeneration,
        snapshot = this.editSnapshot();
      void this.checkDraftValidation(this.definition).then((error) => {
        if (
          !this.destroyed &&
          generation === this.documentGeneration &&
          snapshot === this.editSnapshot()
        ) {
          this.draftValidationError = error;
          this.changeDetector.markForCheck();
        }
      });
    }, 400);
  }
  private async checkDraftValidation(value: WorkflowDefinition): Promise<string> {
    const request = ++this.validationRequest,
      generation = this.documentGeneration;
    const snapshot = this.definitionSnapshot(value);
    const current = () =>
      !this.destroyed &&
      request === this.validationRequest &&
      generation === this.documentGeneration &&
      snapshot === this.definitionSnapshot();
    try {
      await this.api.validate(structuredClone(value));
      if (current()) this.recordNodeErrors();
      return '';
    } catch (error) {
      if (current()) this.recordNodeErrors(error);
      return this.errorText(error);
    }
  }

  async validate(): Promise<boolean> {
    if (!this.edit.canEditWorkflow) return false;
    if (!this.canSaveEditors()) return false;
    const generation = this.documentGeneration;
    const snapshot = this.editSnapshot();
    try {
      const response = await this.api.validate(structuredClone(this.definition));
      if (!this.canApplyResponse(generation, snapshot)) return false;
      this.definition = response.definition;
      this.syncJson();
      this.draftValidationError = '';
      this.recordNodeErrors();
      this.notify('Everything looks good. This workflow is ready to publish.');
      return true;
    } catch (error) {
      if (
        !this.destroyed &&
        generation === this.documentGeneration &&
        snapshot === this.editSnapshot()
      ) {
        this.draftValidationError = this.errorText(error);
        this.recordNodeErrors(error);
      }
      this.handleError(error);
      return false;
    } finally {
      this.changeDetector.markForCheck();
    }
  }

  openJson(): void {
    if (this.jsonOpen) return;
    this.syncJson();
    this.jsonEditorSnapshot = this.jsonDraft;
    this.jsonOpen = true;
  }

  async closeJson(): Promise<void> {
    if (
      this.hasUnappliedJson &&
      !(await this.dialogs.confirm('Discard your unapplied JSON changes?', {
        title: 'Discard JSON changes?',
        confirmText: 'Discard changes',
        cancelText: 'Keep editing',
        danger: true,
      }))
    )
      return;
    this.jsonOpen = false;
    this.syncJson();
    this.autosave.changed();
    this.changeDetector.markForCheck();
  }

  private canSaveEditors(): boolean {
    if (
      !this.hasUnappliedJson &&
      this.editor.stepDataDrafts.size === 0 &&
      this.editor.schemaDrafts.size === 0
    )
      return true;
    this.notify(
      'Apply or discard your JSON editor changes and fix invalid node input JSON before saving.',
      true,
    );
    return false;
  }

  async applyJson(): Promise<void> {
    if (!this.edit.canEditWorkflow) return;
    try {
      const definition = JSON.parse(this.jsonDraft) as WorkflowDefinition;
      if (
        !definition ||
        !Array.isArray(definition.steps) ||
        !definition.trigger ||
        typeof definition.id !== 'string' ||
        typeof definition.name !== 'string'
      ) {
        throw new Error('A workflow needs an ID, name, trigger and steps array.');
      }
      if (
        definition.signal_handlers !== undefined &&
        (!Array.isArray(definition.signal_handlers) ||
          definition.signal_handlers.length > 20 ||
          definition.signal_handlers.some(
            (handler) =>
              !handler ||
              typeof handler.id !== 'string' ||
              typeof handler.signal_name !== 'string' ||
              !Array.isArray(handler.steps),
          ))
      ) {
        throw new Error(
          'signal_handlers must be an array of at most 20 handlers with id, signal_name and steps.',
        );
      }
      if ((!this.isNewWorkflow || this.edit.editingToken) && definition.id !== this.definition.id) {
        throw new Error(
          'The ID of an existing workflow cannot be changed. Create a new workflow to import a copy.',
        );
      }
      if (
        (this.editor.stepDataDrafts.size || this.editor.schemaDrafts.size) &&
        !(await this.dialogs.confirm(
          'Discard invalid node input/schema JSON and apply this workflow?',
          { title: 'Replace node input?', confirmText: 'Apply workflow', danger: true },
        ))
      )
        return;
      if (!this.edit.canEditWorkflow) return;
      this.replaceDefinition(definition, false, this.workflowProjectId, true);
      this.editor.selectedIndex = this.definition.steps.length ? 0 : -1;
      this.jsonOpen = false;
      this.markChanged();
      this.notify('JSON applied to the visual builder.');
    } catch (error) {
      this.notify(`That JSON is not valid: ${this.errorText(error)}`, true);
    }
  }

  private readonly bridge = inject(WorkspaceBridge);
  async openRunDashboard(): Promise<void> {
    await this.bridge.host?.openManagement('runs');
  }

  private async refreshHealth(): Promise<void> {
    try {
      const health = await this.api.health();
      this.connected = health.temporal && (health.database ?? true);
      this.databaseDriver = health.database_driver ?? 'file';
    } catch {
      this.connected = false;
    } finally {
      this.changeDetector.markForCheck();
    }
  }

  async loadDefinitions(): Promise<void> {
    this.definitionsLoading = true;
    this.definitionsError = '';
    try {
      this.definitions = await this.api.listDefinitions();
    } catch (error) {
      this.definitionsError = 'Check your connection and team access, then try again.';
      this.handleError(error);
    } finally {
      this.definitionsLoading = false;
      this.changeDetector.markForCheck();
    }
  }

  get visibleDefinitions(): DefinitionSummary[] {
    const query = this.workflowSearch.trim().toLowerCase();
    return this.definitions.filter(
      (item) =>
        (item.project_id ?? 'default') === this.activeProjectId &&
        (!query || `${item.name} ${item.id}`.toLowerCase().includes(query)),
    );
  }

  projectName(id: string): string {
    return this.projects.find((p) => p.id === id)?.name ?? 'General';
  }

  async openProject(id: string): Promise<void> {
    if (this.router) {
      if (!this.projectBusy) await this.router.navigateByUrl(projectUrl(id));
      return;
    }
    if (await this.selectProject(id)) this.projectOpened = true;
    this.changeDetector.markForCheck();
  }

  async backToProjects(): Promise<void> {
    if (this.router) await this.router.navigateByUrl('/workflows/projects');
    else await this.showDashboard();
  }

  async selectProject(id: string, fromRoute = false): Promise<boolean> {
    if (
      this.projectBusy ||
      this.versions.saving ||
      this.edit.acquiringEdit ||
      this.openingDefinitionId
    )
      return false;
    if (!fromRoute && this.router) return this.router.navigateByUrl(projectUrl(id));
    if (id === this.activeProjectId) return this.showDashboard(false);
    if (!(await this.confirmDiscardChanges())) return false;
    await this.edit.releaseEditing();
    this.edit.editLost = false;
    this.edit.editLock = undefined;
    this.activeProjectId = id;
    this.workflowSearch = '';
    this.replaceDefinition(emptyDefinition(), true, id);
    this.testRun.run = undefined;
    this.versions.definitionHistory = undefined;
    this.testRun.stopPolling();
    this.testRun.runOpen = false;
    this.versions.versionOpen = false;
    this.builderOpen = false;
    this.message = '';
    this.changeDetector.markForCheck();
    return true;
  }

  async createProject(): Promise<void> {
    if (this.projectBusy || !this.auth.canEdit()) return;
    const name = await this.dialogs.prompt('Choose a name to organize your workflows.', {
      title: 'New project',
      confirmText: 'Create project',
    });
    if (name === null) return;
    this.projectBusy = true;
    try {
      const project = await this.api.createProject(name);
      if (this.destroyed) return;
      await this.loadProjects();
      // Keep the current editor intact. Opening the new folder is a separate,
      // guarded navigation so creating a folder cannot discard an open draft.
      this.notify(`Project “${project.name}” created. Select its folder to add workflows.`);
    } catch (error) {
      this.handleError(error);
    } finally {
      this.projectBusy = false;
      this.changeDetector.markForCheck();
    }
  }

  async renameProject(id = this.activeProjectId): Promise<void> {
    if (this.projectBusy || !this.auth.canEdit()) return;
    const name = await this.dialogs.prompt('Update the name of this project folder.', {
      title: 'Rename project',
      confirmText: 'Save name',
      initialValue: this.projectName(id),
    });
    if (name === null) return;
    this.projectBusy = true;
    try {
      const project = await this.api.renameProject(id, name);
      if (this.destroyed) return;
      await this.loadProjects();
      this.notify(`Project renamed to “${project.name}”.`);
    } catch (error) {
      this.handleError(error);
    } finally {
      this.projectBusy = false;
      this.changeDetector.markForCheck();
    }
  }

  async moveWorkflowProject(id: string, select?: HTMLSelectElement): Promise<void> {
    const previous = this.workflowProjectId;
    if (!this.edit.canEditWorkflow || this.versions.saving || this.projectBusy || id === previous) {
      if (select) select.value = previous;
      return;
    }
    if (!this.definitions.some((item) => item.id === this.definition.id)) {
      this.workflowProjectId = id;
      this.notify(`This draft will be saved in “${this.projectName(id)}”.`);
      return;
    }
    const generation = this.documentGeneration;
    const definitionId = this.definition.id;
    this.projectBusy = true;
    try {
      await this.api.moveToProject(definitionId, id);
      if (this.destroyed || generation !== this.documentGeneration) return;
      this.workflowProjectId = id;
      this.savedProjectId = id;
      this.activeProjectId = id;
      await this.loadDefinitions();
      this.notify(
        `Workflow moved to “${this.projectName(id)}”.${this.hasUnsavedChanges ? ' Your workflow edits are still unsaved.' : ''}`,
      );
    } catch (error) {
      if (select) select.value = previous;
      this.handleError(error);
    } finally {
      this.projectBusy = false;
      this.changeDetector.markForCheck();
    }
  }

  async reloadProjects(): Promise<void> {
    await Promise.all([this.loadProjects(), this.loadDefinitions()]);
  }

  private async loadProjects(): Promise<void> {
    this.projectsLoading = true;
    this.projectsError = '';
    try {
      this.projects = await this.api.listProjects();
    } catch (error) {
      this.projectsError = this.errorText(error);
      this.handleError(error);
    } finally {
      this.projectsLoading = false;
      this.changeDetector.markForCheck();
    }
  }

  get publishedDefinitions(): DefinitionSummary[] {
    return this.definitions.filter((item) => item.latest_version > 0);
  }

  get publishedChildren(): DefinitionSummary[] {
    return this.publishedDefinitions.filter((item) => item.published_kind === 'child');
  }

  async openSchedules(): Promise<void> {
    await this.bridge.host?.openManagement('schedules');
  }

  private syncJson(): void {
    this.scheduleValidation();
    if (this.definition.layout) {
      const keys = new Set([
        '@trigger',
        '@finish',
        ...(this.definition.signal_handlers ?? []).flatMap((handler) => [
          '@handler:' + handler.id,
          '@return:' + handler.id,
        ]),
        ...allDefinitionSteps(this.definition).flatMap((step) => [step.id, `@join:${step.id}`]),
      ]);
      for (const key of Object.keys(this.definition.layout.positions))
        if (!keys.has(key)) delete this.definition.layout.positions[key];
    }
    for (const step of allDefinitionSteps(this.definition)) {
      if (step.type === 'condition') {
        step.then ??= [];
        step.else ??= [];
      }
    }
    if (
      this.editor.selectedContainer &&
      this.editor.selectedContainer !== this.definition.steps &&
      !rootPaths(this.definition).some(
        (root) =>
          root === this.editor.selectedContainer ||
          root.some((step) => containsPath(step, this.editor.selectedContainer!)),
      )
    )
      this.editor.selectedContainer = undefined;
    if (this.jsonOpen) return;
    this.jsonDraft = JSON.stringify(this.definition, null, 2);
  }

  private notify(message: string, error = false): void {
    this.messageRevision++;
    this.message = message;
    this.messageError = error;
    this.changeDetector.markForCheck();
  }

  private handleError(error: unknown): void {
    const response = error as { status?: number; error?: { lock?: EditLockStatus } };
    if (response?.status === 423) {
      this.edit.editUntil = 0;
      this.edit.editLost = !!this.edit.editingToken;
      this.edit.editLock = response.error?.lock;
    }
    this.notify(this.errorText(error), true);
  }

  private errorText(error: unknown): string {
    if (error && typeof error === 'object' && 'error' in error) {
      const response = (error as { error?: { detail?: unknown } }).error;
      if (response?.detail) {
        return typeof response.detail === 'string'
          ? response.detail
          : JSON.stringify(response.detail);
      }
    }
    return error instanceof Error ? error.message : 'Something went wrong.';
  }
  readonly stepLabel = stepLabel;
  readonly palette = workflowPalette;
  readonly stepIcon = stepIcon;
  readonly stepTone = stepTone;
  readonly stepSummary = stepSummary;
  readonly versions: WorkflowVersions;
  readonly editor: WorkflowEditor;
  readonly edit: WorkflowEditLock;
  readonly testRun: WorkflowTestRun;
  readonly deletion: LibraryDeletion;
  readonly examples: WorkflowExamples;

  constructor() {
    const app = this;
    const feedback = {
      notify: (message: string, error = false) => app.notify(message, error),
      handleError: (error: unknown) => app.handleError(error),
      errorText: (error: unknown) => app.errorText(error),
      markForCheck: () => app.changeDetector.markForCheck(),
    };
    this.examples = new WorkflowExamples(this.api, {
      ...feedback,
      get destroyed() {
        return app.destroyed;
      },
      get generation() {
        return app.documentGeneration;
      },
      get projectId() {
        return app.activeProjectId;
      },
      get busy() {
        return app.projectBusy;
      },
      set busy(value) {
        app.projectBusy = value;
      },
      canCreate: () =>
        app.auth.canEdit() &&
        !app.builderOpen &&
        !app.openingDefinitionId &&
        !app.versions.saving &&
        !app.projectsLoading &&
        !app.definitionsLoading,
      confirm: (message, options) => app.dialogs.confirm(message, options),
      reload: async () => {
        await app.loadDefinitions();
        if (app.definitionsError) throw new Error(app.definitionsError);
      },
      accept: async (definition, input, projectId) => {
        if (app.router && app.routeDefinitionId !== definition.id) {
          const opened = await app.router.navigateByUrl(definitionUrl(definition.id), {
            state: { runloomExample: { workflowId: definition.id, input } },
          });
          if (!opened)
            throw new Error('Example saved. Open its Details from the workflow list to continue.');
          return;
        }
        app.replaceDefinition(definition, true, projectId);
        app.versions.definitionHistory = undefined;
        app.resetDetailPanels();
        app.builderOpen = true;
        app.testRun.runInput = JSON.stringify(input, null, 2);
        app.syncJson();
      },
    });
    this.deletion = new LibraryDeletion(this.api, {
      ...feedback,
      get canDelete() {
        return (
          app.auth.canEdit() &&
          !app.builderOpen &&
          !app.openingDefinitionId &&
          !app.versions.saving &&
          !app.definitionsLoading &&
          !app.projectsLoading
        );
      },
      get destroyed() {
        return app.destroyed;
      },
      get generation() {
        return app.documentGeneration;
      },
      get projects() {
        return app.projects;
      },
      get workflows() {
        return app.definitions;
      },
      get busy() {
        return app.projectBusy;
      },
      set busy(value) {
        app.projectBusy = value;
      },
      confirm: (message, options) => app.dialogs.confirm(message, options),
      reload: () => app.reloadProjects(),
      projectDeleted: (id) => {
        if (app.activeProjectId === id) {
          app.activeProjectId = 'default';
          app.projectOpened = false;
          app.replaceDefinition(emptyDefinition(), true, 'default');
        }
      },
    });

    this.autosave = new WorkflowAutosave({
      get eligible() {
        return !app.destroyed && app.builderOpen && !app.isNewWorkflow && app.edit.canEditWorkflow;
      },
      get dirty() {
        return app.hasUnsavedChanges;
      },
      get busy() {
        return app.versions.saving || app.projectBusy || !!app.openingDefinitionId;
      },
      get blocked() {
        return (
          app.jsonOpen ||
          !!app.editor.stepDataDrafts.size ||
          !!app.editor.schemaDrafts.size ||
          app.testRun.runOpen ||
          app.versions.versionOpen ||
          !!document.querySelector('dialog[open]')
        );
      },
      get generation() {
        return app.documentGeneration;
      },
      save: async () => {
        await app.versions.save(true);
        return app.versions.lastWriteSucceeded;
      },
      markForCheck: feedback.markForCheck,
    });
    this.versions = new WorkflowVersions(this.api, {
      ...feedback,
      get definition() {
        return app.definition;
      },
      get projectBusy() {
        return app.projectBusy;
      },
      get canEditWorkflow() {
        return app.edit.canEditWorkflow;
      },
      get ownsEdit() {
        return app.edit.ownsEdit;
      },
      get documentGeneration() {
        return app.documentGeneration;
      },
      get workflowProjectId() {
        return app.workflowProjectId;
      },
      get destroyed() {
        return app.destroyed;
      },
      get hasUnsavedChanges() {
        return app.hasUnsavedChanges;
      },
      get draftValidationError() {
        return app.draftValidationError;
      },
      set draftValidationError(value: string) {
        app.draftValidationError = value;
      },
      canSaveEditors: () => app.canSaveEditors(),
      editSnapshot: () => app.editSnapshot(),
      ensureNewDraftLock: () => app.edit.ensureNewDraftLock(),
      saveSettled: () => {
        if (app.versions.lastWriteSucceeded) app.autosave.changed();
      },
      acceptSaved: (definition, snapshot, projectId) => {
        app.autosave.reset();
        if (app.isNewWorkflow) {
          app.history.reset(app.definitionSnapshot(definition));
          app.history.record(app.definitionSnapshot());
        }
        if (snapshot === app.editSnapshot()) {
          app.definition = definition;
          app.syncJson();
        }
        app.savedSnapshot = app.definitionSnapshot(definition);
        if (projectId !== undefined) app.savedProjectId = projectId;
      },
      loadDefinitions: () => app.loadDefinitions(),
      checkDraftValidation: (definition) => app.checkDraftValidation(definition),
      confirmDiscardChanges: () => app.confirmDiscardChanges(),
      confirm: (message, options) => app.dialogs.confirm(message, options),
      advanceGeneration: () => ++app.documentGeneration,
      canApplyResponse: (generation, snapshot) => app.canApplyResponse(generation, snapshot),
      replaceDefinition: (definition) => app.replaceDefinition(definition),
      selectFirstStep: () => {
        app.editor.selectedIndex = app.definition.steps.length ? 0 : -1;
      },
      syncJson: () => app.syncJson(),
    });
    this.editor = new WorkflowEditor({
      ...feedback,
      get definition() {
        return app.definition;
      },
      get canEditWorkflow() {
        return app.edit.canEditWorkflow;
      },
      get saving() {
        return app.versions.saving;
      },
      get documentGeneration() {
        return app.documentGeneration;
      },
      get definitions() {
        return app.definitions;
      },
      get publishedChildren() {
        return app.publishedChildren;
      },
      get webhookUrl() {
        return app.webhookUrl;
      },
      markChanged: () => app.markChanged(),
      confirm: (message, options) => app.dialogs.confirm(message, options),
    });
    this.edit = new WorkflowEditLock(this.api, {
      ...feedback,
      get definition() {
        return app.definition;
      },
      get isNewWorkflow() {
        return app.isNewWorkflow;
      },
      get saving() {
        return app.versions.saving;
      },
      get projectBusy() {
        return app.projectBusy;
      },
      get documentGeneration() {
        return app.documentGeneration;
      },
      get destroyed() {
        return app.destroyed;
      },
      get builderOpen() {
        return app.builderOpen;
      },
      canEdit: () => app.auth.canEdit(),
      confirmDiscardChanges: () => app.confirmDiscardChanges(),
      replaceDefinition: (definition, saved) => app.replaceDefinition(definition, saved),
      openDefinition: (id, discardConfirmed) => app.openDefinition(id, discardConfirmed),
    });
    this.testRun = new WorkflowTestRun(this.api, {
      ...feedback,
      get definition() {
        return app.definition;
      },
      get isNewWorkflow() {
        return app.isNewWorkflow;
      },
      get documentGeneration() {
        return app.documentGeneration;
      },
      get destroyed() {
        return app.destroyed;
      },
      canEdit: () => app.auth.canEdit(),
      canSaveEditors: () => app.canSaveEditors(),
    });
  }
}
