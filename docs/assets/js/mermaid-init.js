// Re-initialize Mermaid on each Material instant-navigation page load
// and attach fullscreen + pan/zoom to every rendered diagram.

document$.subscribe(() => {
  if (typeof mermaid === "undefined") return;
  const nodes = document.querySelectorAll("pre.mermaid > code");
  if (!nodes.length) return;

  mermaid.initialize({ startOnLoad: false, theme: "neutral" });

  // Convert <pre class="mermaid"><code>…</code></pre> → wrapped structure
  const pending = [];
  nodes.forEach((node) => {
    const pre = node.parentElement;

    const wrapper = document.createElement("div");
    wrapper.className = "diagram-wrapper";

    const mermaidDiv = document.createElement("div");
    mermaidDiv.className = "mermaid";
    mermaidDiv.textContent = node.textContent;

    const btn = buildFullscreenButton();

    wrapper.appendChild(mermaidDiv);
    wrapper.appendChild(btn);
    pre.replaceWith(wrapper);

    pending.push({ mermaidDiv, btn });
  });

  mermaid.run({ querySelector: ".mermaid" }).then(() => {
    pending.forEach(({ mermaidDiv, btn }) => {
      const svg = mermaidDiv.querySelector("svg");
      if (!svg) return;
      btn.addEventListener("click", () => openOverlay(svg));
    });
  });
});

// ── Fullscreen button ─────────────────────────────────────────────────────

function buildFullscreenButton() {
  const btn = document.createElement("button");
  btn.className = "diagram-fullscreen-btn";
  btn.setAttribute("aria-label", "View diagram fullscreen");
  btn.setAttribute("title", "View fullscreen");
  btn.innerHTML =
    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
      "<polyline points=\"15 3 21 3 21 9\"/>" +
      "<polyline points=\"9 21 3 21 3 15\"/>" +
      "<line x1=\"21\" y1=\"3\" x2=\"14\" y2=\"10\"/>" +
      "<line x1=\"3\" y1=\"21\" x2=\"10\" y2=\"14\"/>" +
    "</svg>" +
    "Fullscreen";
  return btn;
}

// ── Overlay ───────────────────────────────────────────────────────────────

function openOverlay(originalSvg) {
  // Clone the SVG so the original stays in place
  const svgClone = originalSvg.cloneNode(true);

  // Remove inline size constraints so svg-pan-zoom can take control
  svgClone.removeAttribute("style");
  svgClone.removeAttribute("width");
  svgClone.removeAttribute("height");

  const overlay = document.createElement("div");
  overlay.className = "diagram-overlay";
  overlay.setAttribute("role", "dialog");
  overlay.setAttribute("aria-modal", "true");
  overlay.setAttribute("aria-label", "Diagram fullscreen view");

  const bar = document.createElement("div");
  bar.className = "diagram-overlay-bar";

  const hint = document.createElement("span");
  hint.className = "diagram-overlay-hint";
  hint.textContent = "Scroll to zoom · drag to pan · double-click to zoom in";

  const closeBtn = document.createElement("button");
  closeBtn.className = "diagram-overlay-close";
  closeBtn.setAttribute("aria-label", "Close fullscreen");
  closeBtn.innerHTML = "&#x2715; Close";
  closeBtn.addEventListener("click", () => overlay.remove());

  bar.appendChild(hint);
  bar.appendChild(closeBtn);

  const viewport = document.createElement("div");
  viewport.className = "diagram-overlay-viewport";
  viewport.appendChild(svgClone);

  overlay.appendChild(bar);
  overlay.appendChild(viewport);
  document.body.appendChild(overlay);

  // Close on backdrop click
  overlay.addEventListener("click", (e) => {
    if (e.target === overlay) overlay.remove();
  });

  // Close on Escape
  const onEsc = (e) => {
    if (e.key === "Escape") {
      overlay.remove();
      document.removeEventListener("keydown", onEsc);
    }
  };
  document.addEventListener("keydown", onEsc);

  // Attach svg-pan-zoom after the overlay is rendered in the DOM
  requestAnimationFrame(() => {
    if (typeof svgPanZoom === "undefined") return;

    // Give the SVG explicit pixel dimensions so svg-pan-zoom can work
    const vpW = viewport.clientWidth || window.innerWidth;
    const vpH = viewport.clientHeight || (window.innerHeight - 50);
    svgClone.setAttribute("width", vpW);
    svgClone.setAttribute("height", vpH);

    const instance = svgPanZoom(svgClone, {
      zoomEnabled: true,
      controlIconsEnabled: true,
      fit: true,
      center: true,
      minZoom: 0.2,
      maxZoom: 20,
      zoomScaleSensitivity: 0.3,
      dblClickZoomEnabled: true,
      mouseWheelZoomEnabled: true,
    });

    // Re-fit on window resize
    const onResize = () => {
      const w = viewport.clientWidth || window.innerWidth;
      const h = viewport.clientHeight || (window.innerHeight - 50);
      svgClone.setAttribute("width", w);
      svgClone.setAttribute("height", h);
      instance.resize();
      instance.fit();
      instance.center();
    };
    window.addEventListener("resize", onResize);

    // Clean up resize listener when overlay is removed from DOM
    const observer = new MutationObserver(() => {
      if (!document.body.contains(overlay)) {
        window.removeEventListener("resize", onResize);
        observer.disconnect();
      }
    });
    observer.observe(document.body, { childList: true });
  });
}
