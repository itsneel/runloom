import assert from 'node:assert/strict';
import { readdirSync, readFileSync, existsSync } from 'node:fs';
import { resolve, dirname, relative, isAbsolute } from 'node:path';
import { fileURLToPath } from 'node:url';
import ts from 'typescript';

const root = fileURLToPath(new URL('../', import.meta.url));
const inside = (path) =>
  !relative(root, path).startsWith('..') && !isAbsolute(relative(root, path));
function walk(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) =>
    entry.isDirectory() ? walk(resolve(dir, entry.name)) : [resolve(dir, entry.name)],
  );
}
const pkg = JSON.parse(readFileSync(resolve(root, 'package.json'), 'utf8'));
assert(!pkg.workspaces, 'Application must not be a workspace');
for (const [name, version] of Object.entries({ ...pkg.dependencies, ...pkg.devDependencies })) {
  assert(
    !version.startsWith('workspace:') && !version.startsWith('link:'),
    `Local source dependency: ${name}`,
  );
  if (version.startsWith('file:')) {
    const path = resolve(root, version.slice(5));
    assert(
      inside(path) && path.endsWith('.tgz') && existsSync(path),
      `Dependency must be a packaged artifact inside this project: ${name}`,
    );
  }
}
const angular = JSON.parse(readFileSync(resolve(root, 'angular.json'), 'utf8'));
assert.equal(
  Object.keys(angular.projects).length,
  1,
  'One independently built Angular application per project',
);
const config = JSON.parse(readFileSync(resolve(root, 'tsconfig.json'), 'utf8'));
assert(!config.compilerOptions.paths, 'No sibling-source aliases');
assert(!config.extends, 'Project configuration must be self-contained');
let components = 0;
const sourceRoot = resolve(root, 'src');
for (const entry of readdirSync(sourceRoot, { withFileTypes: true })) {
  assert(
    entry.isDirectory() ? entry.name === 'app' : ['main.ts', 'index.html'].includes(entry.name),
    `src/ is for entry files only; move ${entry.name} into app/core, app/layout, app/features or app/shared`,
  );
}
for (const file of walk(resolve(root, 'src')).filter((file) => file.endsWith('.ts'))) {
  const ast = ts.createSourceFile(file, readFileSync(file, 'utf8'), ts.ScriptTarget.Latest, true);
  function visit(node) {
    const moduleSpecifier =
      ts.isImportDeclaration(node) || ts.isExportDeclaration(node)
        ? node.moduleSpecifier
        : ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword
          ? node.arguments[0]
          : undefined;
    if (moduleSpecifier && ts.isStringLiteral(moduleSpecifier)) {
      const specifier = moduleSpecifier.text;
      assert(!isAbsolute(specifier), `${file}: absolute source dependency`);
      if (specifier.startsWith('.')) {
        const target = resolve(dirname(file), specifier);
        assert(inside(target), `${file}: import leaves project: ${specifier}`);
        assert(
          [target, target + '.ts', resolve(target, 'index.ts')].some(existsSync),
          `${file}: unresolved local import: ${specifier}`,
        );
        const sourcePath = relative(sourceRoot, file),
          targetPath = relative(sourceRoot, target);
        if (/^app\/features\/[^/]+\/(state|services)\//.test(sourcePath)) {
          assert(
            !targetPath.endsWith('.component') && !targetPath.includes('/layout/'),
            `${file}: feature state/services must use typed ports, not component or shell imports`,
          );
        }
        if (/^app\/(core|shared)\//.test(sourcePath)) {
          assert(
            !/^app\/(features|layout|federation)\//.test(targetPath),
            `${file}: core/shared must not depend on feature or layout implementation`,
          );
        }
        if (sourcePath.startsWith('app/features/')) {
          assert(
            !targetPath.startsWith('app/layout/'),
            `${file}: features must not import application shells`,
          );
        }
      } else
        assert(
          Object.keys({ ...pkg.dependencies, ...pkg.devDependencies }).some(
            (name) => specifier === name || specifier.startsWith(name + '/'),
          ),
          `${file}: undeclared dependency ${specifier}`,
        );
    }
    if (
      ts.isDecorator(node) &&
      ts.isCallExpression(node.expression) &&
      node.expression.expression.getText(ast) === 'Component'
    ) {
      components++;
      assert(file.endsWith('.component.ts'), `${file}: use the .component.ts suffix`);
      assert(
        /^app\/(layout|features|shared\/components)\//.test(relative(sourceRoot, file)),
        `${file}: place components within layout, features or shared/components`,
      );
      const props = node.expression.arguments[0].properties;
      assert(
        props.some((p) => p.name?.getText(ast) === 'templateUrl'),
        `${file}: external HTML required`,
      );
      assert(
        props.some((p) => ['styleUrl', 'styleUrls'].includes(p.name?.getText(ast))),
        `${file}: external CSS/SCSS required`,
      );
      for (const p of props) {
        const name = p.name?.getText(ast);
        assert(
          !['template', 'styles'].includes(name),
          `${file}: inline component resources forbidden`,
        );
        if (['templateUrl', 'styleUrl', 'styleUrls'].includes(name))
          for (const item of name === 'styleUrls' ? p.initializer.elements : [p.initializer]) {
            const resource = resolve(dirname(file), item.text);
            assert(
              inside(resource) && existsSync(resource),
              `${file}: external/missing component resource`,
            );
          }
      }
    }
    ts.forEachChild(node, visit);
  }
  visit(ast);
}
console.log(
  `PASS ${pkg.name}: independent source/configuration and ${components} external-template components`,
);
