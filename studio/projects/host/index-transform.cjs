// Federated Webpack output is ESM, including development's stylesheet entry.
module.exports = (_target, html) => html.replace(/<script src="([^"]+)" defer><\/script>/g, '<script src="$1" type="module"></script>');
