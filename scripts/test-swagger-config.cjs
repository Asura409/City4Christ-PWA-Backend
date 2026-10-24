// Execute the real initializer and verify the interactive editor workflow is enabled.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
let options;
function SwaggerUIBundle(config) { options = config; return {}; }
SwaggerUIBundle.presets = { apis: {} };
const context = { window: {}, SwaggerUIBundle };
vm.runInNewContext(fs.readFileSync('internal/app/apidocs/init.js', 'utf8'), context);
context.window.onload();
for (const method of ['get', 'post', 'patch', 'put', 'delete']) {
  assert.ok(options.supportedSubmitMethods.includes(method), `${method} must support Try it out`);
}
assert.equal(options.url, '/openapi.json');
assert.equal(options.withCredentials, true);
assert.equal(options.persistAuthorization, false);
assert.equal(options.validatorUrl, null);
assert.equal(options.queryConfigEnabled, false);
console.log('Swagger UI interactive editor configuration checks passed.');
