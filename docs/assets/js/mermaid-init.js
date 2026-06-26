// Re-initialize Mermaid on each Material instant-navigation page load.
document$.subscribe(() => {
  if (typeof mermaid === "undefined") return;
  const nodes = document.querySelectorAll("pre.mermaid > code");
  if (!nodes.length) return;
  mermaid.initialize({ startOnLoad: false, theme: "neutral" });
  nodes.forEach((node) => {
    // Unwrap: replace <pre class="mermaid"><code>...</code></pre>
    // with <div class="mermaid">...</div> that Mermaid processes
    const pre = node.parentElement;
    const div = document.createElement("div");
    div.className = "mermaid";
    div.textContent = node.textContent;
    pre.replaceWith(div);
  });
  mermaid.run({ querySelector: ".mermaid" });
});
