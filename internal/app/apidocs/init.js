window.onload = function () {
  window.ui = SwaggerUIBundle({
    url: '/openapi.json',
    dom_id: '#swagger-ui',
    deepLinking: true,
    withCredentials: true,
    persistAuthorization: false,
    validatorUrl: null,
    queryConfigEnabled: false,
    supportedSubmitMethods: ['get', 'post', 'patch'],
    presets: [SwaggerUIBundle.presets.apis]
  });
};
