// license-webpack-plugin 4.0.2 expects '=' in provide-module identifiers;
// Webpack >=5.107 uses '|'. Adapt only that virtual identifier, retaining license extraction.
// Upstream reproduction: https://github.com/jupyterlab/jupyterlab/issues/18984
module.exports = function install() {
  const { WebpackInnerModuleIterator } = require('license-webpack-plugin/dist/WebpackInnerModuleIterator');
  const prototype = WebpackInnerModuleIterator.prototype;
  if (prototype.runloomProvideCompatibility) return;
  const original = prototype.getActualFilename;
  prototype.getActualFilename = function (filename) {
    if (filename?.startsWith('provide module') && filename.includes('|')) {
      return filename.slice(filename.indexOf('|') + 1).trim();
    }
    return original.call(this,filename);
  };
  prototype.runloomProvideCompatibility = true;
};
