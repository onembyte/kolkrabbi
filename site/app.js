"use strict";

(() => {
  const button = document.querySelector("[data-copy-target]");

  if (!button) {
    return;
  }

  const label = button.querySelector(".copy-button-label");
  const status = document.getElementById("copy-status");
  const originalLabel = label.textContent;
  let resetTimer;

  const copyWithFallback = (text) => {
    const source = document.createElement("textarea");
    source.value = text;
    source.className = "copy-source";
    source.setAttribute("aria-hidden", "true");
    source.setAttribute("readonly", "");
    document.body.appendChild(source);
    source.select();

    let copied = false;
    try {
      copied = document.execCommand("copy");
    } finally {
      source.remove();
    }

    if (!copied) {
      throw new Error("The browser rejected the copy command.");
    }
  };

  const writeClipboard = async (text) => {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return;
    }

    copyWithFallback(text);
  };

  const resetButton = () => {
    label.textContent = originalLabel;
    button.removeAttribute("data-state");
    status.textContent = "";
  };

  button.addEventListener("click", async () => {
    const target = document.getElementById(button.dataset.copyTarget);
    if (!target) {
      return;
    }

    window.clearTimeout(resetTimer);
    button.disabled = true;

    try {
      await writeClipboard(target.textContent.trim());
      label.textContent = "Copied";
      button.dataset.state = "copied";
      status.textContent = "Install command copied to clipboard.";
      resetTimer = window.setTimeout(resetButton, 2200);
    } catch (_error) {
      label.textContent = "Try again";
      status.textContent = "Copy failed. Select and copy the install command manually.";
      resetTimer = window.setTimeout(resetButton, 3200);
    } finally {
      button.disabled = false;
    }
  });
})();
