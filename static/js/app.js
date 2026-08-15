// Questlog UI helpers. Kept intentionally tiny: HTMX handles the
// data-driven interactions (catalog search/auto-fill); this file only
// covers pure UI chrome that HTMX can't express declaratively.
(function () {
  "use strict";

  // Library filter slide-over.
  function setFilter(open) {
    var overlay = document.getElementById("filter-overlay");
    var backdrop = document.getElementById("filter-backdrop");
    var panel = document.getElementById("filter-panel");
    if (!overlay || !backdrop || !panel) return;
    overlay.classList.toggle("pointer-events-none", !open);
    backdrop.classList.toggle("opacity-0", !open);
    backdrop.classList.toggle("opacity-100", open);
    panel.classList.toggle("translate-x-full", !open);
    document.body.style.overflow = open ? "hidden" : "";
  }

  window.qlOpenFilter = function () { setFilter(true); };
  window.qlCloseFilter = function () { setFilter(false); };
  window.qlClearRating = function () {
    document.querySelectorAll('input[name="rating"]').forEach(function (r) {
      r.checked = false;
    });
  };

  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape") setFilter(false);
  });
})();
