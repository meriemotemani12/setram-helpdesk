window.addEventListener("load", function () {
  SwaggerUIBundle({
    url: window.location.origin + "/openapi.yaml",
    dom_id: "#swagger-ui",
    deepLinking: true,
    presets: [
      SwaggerUIBundle.presets.apis,
      SwaggerUIStandalonePreset
    ],
    layout: "StandaloneLayout"
  });
});
