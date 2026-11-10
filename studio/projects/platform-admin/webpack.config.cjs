require('./license-compat.cjs')();
const { ModuleFederationPlugin } = require('webpack').container;
const { share } = require('@angular-architects/module-federation/webpack');
const singleton = { singleton: true, strictVersion: true, requiredVersion: 'auto' };
const platformVersion = require('@runloom/studio-platform/package.json').version;
module.exports = {
  output: { uniqueName: 'runloom_platform_admin', publicPath: 'auto' },
  optimization: { runtimeChunk: false },
  experiments: { outputModule: true },
  plugins: [
    new ModuleFederationPlugin({
      name: 'runloom_platform_admin',
      library: { type: 'module' },
      filename: 'remoteEntry.js',
      exposes: { './Application': './src/app/federation/entry.ts' },
      shared: {
        ...share({
          '@angular/core': singleton,
          '@angular/common': { ...singleton, includeSecondaries: true },
          '@angular/forms': singleton,
          '@angular/platform-browser': singleton,
          '@angular/router': singleton,
          rxjs: { ...singleton, includeSecondaries: true },
        }),
        '@runloom/studio-platform': {
          singleton: true,
          strictVersion: true,
          requiredVersion: platformVersion,
        },
      },
    }),
  ],
};
