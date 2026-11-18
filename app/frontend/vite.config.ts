import { defineConfig, type Plugin } from "vite";
import wails from "@wailsio/runtime/plugins/vite";

// Content-Security-Policy for production builds: everything from the app
// itself, no remote scripts, fonts, frames or navigation targets. Dev mode
// (Vite HMR) is left without CSP.
const csp = [
  "default-src 'self'",
  "script-src 'self'",
  "style-src 'self'",
  "font-src 'self'",
  "img-src 'self' data:",
  "connect-src 'self'",
  "object-src 'none'",
  "frame-src 'none'",
  "base-uri 'none'",
  "form-action 'none'",
].join("; ");

function contentSecurityPolicy(): Plugin {
  return {
    name: "ra-csp",
    apply: "build",
    transformIndexHtml(html) {
      return html.replace(
        '<meta charset="utf-8">',
        `<meta charset="utf-8">\n    <meta http-equiv="Content-Security-Policy" content="${csp}">`,
      );
    },
  };
}

export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  build: {
    // Inline nothing as data: URIs; fonts stay separate files (CSP font-src 'self').
    assetsInlineLimit: 0,
  },
  plugins: [wails("./bindings"), contentSecurityPolicy()],
});
