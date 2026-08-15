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

  // Password reveal toggle (login form). Pure UI chrome, like the
  // rating-clear helper above.
  window.qlTogglePassword = function (btn) {
    var input = btn.parentElement.querySelector("input");
    var reveal = input.type === "password";
    input.type = reveal ? "text" : "password";
    btn.setAttribute("aria-label", reveal ? "Hide password" : "Show password");
    btn.setAttribute("title", reveal ? "Hide password" : "Show password");
    btn.querySelector(".ql-pw-eye").classList.toggle("hidden", reveal);
    btn.querySelector(".ql-pw-eye-off").classList.toggle("hidden", !reveal);
    input.focus();
  };

  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape") setFilter(false);
  });
})();
