// Restore the last theme's variables before first paint (themes.ts writes them).
// A file rather than an inline script so the CSP can forbid inline scripts.
try {
  var v = localStorage.getItem("strata.vars");
  if (v) document.documentElement.style.cssText = v;
} catch (e) {}
