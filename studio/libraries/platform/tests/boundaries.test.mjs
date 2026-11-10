import assert from 'node:assert/strict';
import {readdirSync, readFileSync, existsSync} from 'node:fs';
import {resolve, dirname, relative} from 'node:path';
import {fileURLToPath} from 'node:url';
import test from 'node:test';
import ts from 'typescript';

const root=fileURLToPath(new URL('../',import.meta.url));
const read=file=>readFileSync(resolve(root,file),'utf8');
function walk(dir){return readdirSync(dir,{withFileTypes:true}).flatMap(entry=>entry.isDirectory()?walk(resolve(dir,entry.name)):[resolve(dir,entry.name)]);}
test('library source is independent and components keep external templates/styles',()=>{
  const pkg=JSON.parse(read('package.json'));
  const dependencies={...pkg.dependencies,...pkg.peerDependencies};
  assert(!pkg.workspaces);
  assert(!JSON.parse(read('tsconfig.json')).extends);
  for(const version of Object.values(dependencies)) assert(!/^(file:|link:|workspace:)/.test(version));
  let components=0;
  for(const file of walk(resolve(root,'src')).filter(file=>file.endsWith('.ts'))){
    const ast=ts.createSourceFile(file,readFileSync(file,'utf8'),ts.ScriptTarget.Latest,true);
    function visit(node){
      if((ts.isImportDeclaration(node)||ts.isExportDeclaration(node)) && node.moduleSpecifier && ts.isStringLiteral(node.moduleSpecifier)){
        const specifier=node.moduleSpecifier.text;
        if(specifier.startsWith('.')) assert(!relative(root,resolve(dirname(file),specifier)).startsWith('..'));
        else assert(Object.keys(dependencies).some(name=>specifier===name || specifier.startsWith(name+'/')),`Undeclared runtime dependency ${specifier}`);
      }
      if(ts.isDecorator(node)&&ts.isCallExpression(node.expression)&&node.expression.expression.getText(ast)==='Component'){
        components++;
        const props=node.expression.arguments[0].properties;
        assert(props.some(p=>p.name?.getText(ast)==='templateUrl'));
        assert(props.some(p=>['styleUrl','styleUrls'].includes(p.name?.getText(ast))));
        for(const p of props){
          const name=p.name?.getText(ast);
          assert(!['template','styles'].includes(name));
          if(['templateUrl','styleUrl','styleUrls'].includes(name)) for(const item of name==='styleUrls'?p.initializer.elements:[p.initializer]){
            const path=resolve(dirname(file),item.text);
            assert(!relative(root,path).startsWith('..') && existsSync(path));
          }
        }
      }
      ts.forEachChild(node,visit);
    }
    visit(ast);
  }
  assert(components>0);
});
