window.onload = function () {
  window.ui = SwaggerUIBundle({
    url: '/openapi.json',
    dom_id: '#swagger-ui',
    deepLinking: true,
    withCredentials: true,
    persistAuthorization: false,
    validatorUrl: null,
    queryConfigEnabled: false,
    supportedSubmitMethods: ['get', 'post', 'patch', 'put', 'delete'],
    presets: [SwaggerUIBundle.presets.apis]
  });
};
