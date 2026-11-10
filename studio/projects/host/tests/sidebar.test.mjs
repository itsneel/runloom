import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { loadTypeScript } from './helpers/load-typescript.mjs';

const shellFile = name => readFileSync(new URL(`../src/app/layout/shell/shell.component.${name}`, import.meta.url), 'utf8');

test('Documentation sits in the footer immediately above Appearance and opens a protected new tab', () => {
  const template = shellFile('html');
  const navigation = template.slice(template.indexOf('<div class="sidebar-navigation"'), template.indexOf('<div class="sidebar-footer">'));
  const footer = template.slice(template.indexOf('<div class="sidebar-footer">'));
  assert.doesNotMatch(navigation, /aria-label="(?:Documentation|Consumer API documentation)"/);
  assert.match(footer, /aria-label="Consumer API documentation"[\s\S]*?openDocumentation\('consumer'\)[\s\S]*?aria-label="Documentation"/);
  assert.match(footer, /aria-label="Documentation"[\s\S]*?<\/button>\s*<runloom-theme-picker/);
  assert.match(footer, /<svg class="documentation-icon"[^>]*aria-hidden="true"[^>]*focusable="false"/);
  assert.match(shellFile('scss'), /\.sidebar-collapsed \.documentation-link\s*\{\s*justify-content: center;/);
  assert.equal(template.match(/aria-label="Documentation"/g)?.length, 1);
  assert.match(template, /\(click\)="openDocumentation\(\)"/);
  assert.match(shellFile('ts'), /page === 'consumer' \? `\$\{this\.documentation\.url\(\)\}\/api\/consumer` : this\.documentation\.url\(\)/);
  assert.match(shellFile('ts'), /window\.open\(url, '_blank', 'noopener,noreferrer'\)/);
});

test('Management is a static heading with permanently rendered navigation', () => {
  const template = shellFile('html');
  assert.match(template, /<h2 class="management-heading nav-label">Management<\/h2>/);
  assert.match(template, /<div id="management-links" class="management-links">/);
  assert.doesNotMatch(template, /management-toggle|management-chevron|managementExpanded/);
  assert.doesNotMatch(shellFile('ts'), /managementExpanded/);
});

test('collapsed sidebar hides the Management label but retains its navigation icons', () => {
  const styles = shellFile('scss');
  assert.match(styles, /\.sidebar-collapsed \.nav-label,[^{]+\{\s*display: none;/);
  assert.doesNotMatch(styles, /\.management-links\[hidden\]/);
});

test('the primary navigation exposes one Workflows action', () => {
  const template = shellFile('html');
  assert.equal(template.match(/aria-label="Workflows"/g)?.length, 1);
  assert.match(template, /\(click\)="openWorkflowDashboard\(\)"/);
  assert.match(template, /<span class="nav-text">Workflows<\/span>/);
});

test('Platform console uses the standard navigation row rather than an unstyled footer button', () => {
  const template = shellFile('html');
  const primary = template.match(/<nav class="primary-navigation"[\s\S]*?<\/nav>/)[0];
  assert.match(primary, /aria-label="Platform console"/);
  assert.equal(template.match(/aria-label="Platform console"/g).length, 1);
  assert.doesNotMatch(template.slice(template.indexOf('<div class="sidebar-footer">')), /aria-label="Platform console"/);
  assert.match(template, /!startupReady\(\).*auth.signedIn\(\).* !initialRouteReady\(\)/);
});

test('sidebar contains no route-dependent library or spacer and scrolls independently of its footer', () => {
  const template = shellFile('html');
  assert.doesNotMatch(template, /library-slot|sidebar-spacer|workflowNavigation|rail-only/);
  assert.match(template, /class="sidebar-navigation"/);
  assert.match(template, /class="sidebar-footer"/);
  assert.equal(template.match(/aria-label="Projects"/g)?.length, 1);
  assert.match(shellFile('scss'), /\.sidebar-navigation\s*\{[^}]*min-height: 0;[^}]*overflow-y: auto;/);
  assert.doesNotMatch(shellFile('scss'), /\.sidebar-collapsed \.admin-navigation\s*\{[^}]*margin-top: auto/);
});

function shell({ manage = false, projectsView = false, allowed = true, busy = false, team = true } = {}) {
  const calls = [];
  const { Shell } = loadTypeScript(new URL('../src/app/layout/shell/shell.component.ts', import.meta.url), {
    modules: {
      '@angular/core': {
        Component: () => value => value, Injectable: () => value => value, ViewChild: () => () => {}, HostListener: () => () => {},
      },
      '@angular/common': {}, '@angular/forms': {}, '@angular/router': {},
      '@runloom/studio-platform': {},
    },
  });
  const app = Object.create(Shell.prototype);
  Object.assign(app, {
    manage, projectsView, working: busy, auth: { team: () => team },
    bridge: {
      workflow: () => ({ showDashboard: async () => { calls.push('dashboard'); return allowed; } }),
      management: () => undefined,
    },
    router: { url: manage ? '/management/teams' : projectsView ? '/workflows/projects' : '/workflows', navigateByUrl: async url => { calls.push(url); return allowed; } },
    closeMobile: () => calls.push('close'),
  });
  return { app, calls };
}

test('platform identity resolves after authentication but before initial navigation is released', async () => {
  const {app, calls} = shell();
  app.auth = {initialize: async () => calls.push('auth'), signedIn: () => true};
  app.platformAccess = {refresh: async () => { calls.push('identity'); return {role: 'platform_staff'}; }};
  app.location = {path: () => '/', replaceState: path => calls.push(path)};
  app.startupReady = {set: value => calls.push('ready:' + value)};
  app.cd = {markForCheck: () => {}};
  await app.ngOnInit();
  assert.deepEqual(calls, ['auth', 'identity', '/platform/health', 'ready:true']);
});

test('platform lookup failures do not invent access or block ordinary login', async () => {
  const {app, calls} = shell();
  app.auth = {initialize: async () => {}, signedIn: () => true};
  app.platformAccess = {refresh: async () => { throw new Error('Unavailable'); }};
  app.location = {path: () => '/', replaceState: () => assert.fail('must not redirect')};
  app.startupReady = {set: value => calls.push(value)};
  app.cd = {markForCheck: () => {}};
  await app.ngOnInit();
  assert.deepEqual(calls, [true]);
});

test('Platform console respects navigation cancellation, busy state and mobile dismissal', async () => {
  for (const allowed of [true, false]) {
    const {app, calls} = shell({allowed});
    await app.openPlatform();
    assert.deepEqual(calls, allowed ? ['/platform', 'close'] : ['/platform']);
  }
  const {app, calls} = shell({busy: true});
  await app.openPlatform();
  assert.deepEqual(calls, []);
});

test('Cmd/Ctrl+S is a no-op in the host on every screen, without blocking the editor listener', () => {
  for (const options of [{}, { manage: true }, { projectsView: true }, { team: false }]) {
    const { app, calls } = shell(options);
    for (const modifier of ['metaKey', 'ctrlKey']) {
      let prevented = 0;
      app.preventBrowserSave({
        key: 's', [modifier]: true,
        preventDefault: () => prevented++,
        stopPropagation: () => assert.fail('the editor must still receive Save'),
        stopImmediatePropagation: () => assert.fail('the editor must still receive Save'),
      });
      assert.equal(prevented, 1);
    }
    assert.deepEqual(calls, [], 'the host must not save, navigate, or submit');
  }
});

test('global Save suppression leaves other shortcuts and ordinary typing unchanged', () => {
  const { app } = shell();
  for (const event of [
    {key: 's'}, {key: 'k', metaKey: true}, {key: 's', metaKey: true, shiftKey: true},
    {key: 's', ctrlKey: true, altKey: true},
  ]) app.preventBrowserSave({...event, preventDefault: () => assert.fail('unrelated key')});
});

test('Save suppression is registered in capture phase and removed on shell destruction', () => {
  const source = shellFile('ts');
  assert.match(source, /document\.addEventListener\('keydown', this\.preventBrowserSave, true\)/);
  assert.match(source, /document\.removeEventListener\('keydown', this\.preventBrowserSave, true\)/);
});

test('dashboard navigation delegates to the builder guard and keeps canceled navigation open', async () => {
  for (const allowed of [true, false]) {
    const { app, calls } = shell({ allowed });
    await app.openWorkflowDashboard();
    assert.deepEqual(calls, allowed ? ['dashboard', 'close'] : ['dashboard']);
  }
});

test('dashboard navigation from Management uses the guarded router', async () => {
  for (const allowed of [true, false]) {
    const { app, calls } = shell({ manage: true, allowed });
    await app.openWorkflowDashboard();
    assert.deepEqual(calls, allowed ? ['/workflows', 'close'] : ['/workflows']);
  }
});

test('dashboard navigation cannot run when busy or without a team', async () => {
  for (const options of [{ busy: true }, { team: false }]) {
    const { app, calls } = shell(options);
    await app.openWorkflowDashboard();
    assert.deepEqual(calls, []);
  }
});

test('Projects has a guarded route and reselecting it resets the view only after discard confirmation', async () => {
  for (const allowed of [true, false]) {
    const first = shell({ allowed });
    await first.app.openProjects();
    assert.deepEqual(first.calls, allowed ? ['/workflows/projects', 'close'] : ['/workflows/projects']);
    const current = shell({ allowed, projectsView: true });
    await current.app.openProjects();
    assert.deepEqual(current.calls, allowed ? ['dashboard', 'close'] : ['dashboard']);
    const dashboard = shell({ allowed, projectsView: true });
    await dashboard.app.openWorkflowDashboard();
    assert.deepEqual(dashboard.calls, allowed ? ['/workflows', 'close'] : ['/workflows']);
  }
  for (const options of [{ busy: true }, { team: false }]) {
    const {app, calls} = shell(options);
    await app.openProjects();
    assert.deepEqual(calls, []);
  }
});
