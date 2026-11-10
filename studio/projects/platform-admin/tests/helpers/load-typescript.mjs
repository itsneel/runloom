import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';
import ts from 'typescript';

// Project-local loader: real feature modules, explicit infrastructure substitutes.
export function loadTypeScript(entry, { modules = {}, globals = {} } = {}) {
  const cache = new Map();
  function load(file) {
    if (cache.has(file)) return cache.get(file);
    const exports = {};
    cache.set(file, exports);
    const source = readFileSync(file, 'utf8');
    const javascript = ts.transpileModule(source, {
      compilerOptions: {
        target: ts.ScriptTarget.ES2022,
        module: ts.ModuleKind.CommonJS,
        experimentalDecorators: true,
      },
    }).outputText;
    vm.runInNewContext(
      javascript,
      {
        exports,
        console,
        crypto,
        structuredClone,
        TextEncoder,
        URLSearchParams,
        setTimeout,
        clearTimeout,
        setInterval,
        clearInterval,
        Error,
        ...globals,
        require: (name) => {
          if (name in modules) return modules[name];
          if (name.startsWith('.')) {
            if (name.endsWith('.component')) return {};
            return load(resolve(dirname(file), name + '.ts'));
          }
          throw new Error(`Unexpected dependency in feature test: ${name}`);
        },
      },
      { filename: file },
    );
    return exports;
  }
  return load(fileURLToPath(entry));
}
