"use strict";

const commands = {
  linux: {
    code: "curl -fsSL https://cli.deplexo.com/install.sh | sh",
    shell: "sh / bash / zsh",
    detail:
      "Installs the latest stable release to ~/.local/bin. Run it again to update.",
    script: "./install.sh",
  },
  macos: {
    code: "curl -fsSL https://cli.deplexo.com/install.sh | sh",
    shell: "sh / bash / zsh",
    detail:
      "Installs the latest stable release to ~/.local/bin. Run it again to update.",
    script: "./install.sh",
  },
  windows: {
    code: "& ([scriptblock]::Create((Invoke-RestMethod -ErrorAction Stop 'https://cli.deplexo.com/install.ps1')))",
    shell: "PowerShell",
    detail:
      "Installs the latest stable release to %LOCALAPPDATA%\\Deplexo\\bin. Run it again to update.",
    script: "./install.ps1",
  },
};
const code = document.querySelector("#install-code");
const copy = document.querySelector("#copy");
const status = document.querySelector("#copy-status");
let copyTimer;
function selectOS(os) {
  const command = commands[os];
  document
    .querySelectorAll("[data-os]")
    .forEach((button) =>
      button.setAttribute("aria-pressed", String(button.dataset.os === os)),
    );
  code.textContent = command.code;
  document.querySelector("#shell-label").textContent = command.shell;
  document.querySelector("#install-detail").textContent = command.detail;
  document.querySelector("#script-link").href = command.script;
  copy.textContent = "Copy";
  status.textContent = "";
  clearTimeout(copyTimer);
}
document
  .querySelectorAll("[data-os]")
  .forEach((button) =>
    button.addEventListener("click", () => selectOS(button.dataset.os)),
  );
const platform = navigator.userAgentData?.platform || navigator.platform || "";
selectOS(
  /win/i.test(platform) ? "windows" : /mac/i.test(platform) ? "macos" : "linux",
);
copy.hidden = false;
copy.addEventListener("click", async () => {
  try {
    await navigator.clipboard.writeText(code.textContent);
    copy.textContent = "Copied";
    status.textContent = "Install command copied.";
    clearTimeout(copyTimer);
    copyTimer = setTimeout(() => {
      copy.textContent = "Copy";
    }, 2000);
  } catch {
    status.textContent =
      "Could not copy. Select the command and copy it manually.";
    const selection = window.getSelection();
    const range = document.createRange();
    range.selectNodeContents(code);
    selection.removeAllRanges();
    selection.addRange(range);
  }
});

const releaseStatus = document.querySelector("#release-status");
fetch("https://api.github.com/repos/Deplexo/cli/releases/latest", {
  signal: AbortSignal.timeout(6000),
})
  .then(async (response) => {
    if (response.status === 404) {
      releaseStatus.textContent = "No stable release published yet ↗";
      document
        .querySelector(".install-note")
        .prepend(
          "Installation will be available when the first stable release is published. ",
        );
      return;
    }
    if (!response.ok) return;
    const release = await response.json();
    if (
      /^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/.test(
        release.tag_name,
      ) &&
      !release.draft &&
      !release.prerelease
    ) {
      releaseStatus.textContent = `${release.tag_name} · Latest release ↗`;
    }
  })
  .catch(() => {
    /* Installation links remain usable when the API is unavailable. */
  });
