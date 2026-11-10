import { inject } from '@angular/core';
import { CanActivateChildFn, CanDeactivateFn } from '@angular/router';
import { WorkspaceBridge } from '@runloom/studio-platform';

export const confirmWorkspaceExit: CanDeactivateFn<unknown> = async () => {
  const bridge = inject(WorkspaceBridge);
  const management = bridge.management();
  const workflow = bridge.workflow();
  if (management && !(await management.confirmDiscard())) return false;
  return !workflow || await workflow.confirmDiscardChanges();
};

// Management stays mounted while changing tabs; guard its child route changes too.
export const confirmManagementTabExit: CanActivateChildFn = async () => {
  const management = inject(WorkspaceBridge).management();
  return !management || await management.confirmDiscard();
};
