"use strict";

const commands = {
  linux: {
    code: "curl -fsSL https://cli.deplexo.com/install.sh | sh",
    shell: "sh / bash / zsh",
    destination: "~/.local/bin",
    script: "./install.sh",
  },
  macos: {
    code: "curl -fsSL https://cli.deplexo.com/install.sh | sh",
    shell: "sh / bash / zsh",
    destination: "~/.local/bin",
    script: "./install.sh",
  },
  windows: {
    code: "& ([scriptblock]::Create((Invoke-RestMethod -ErrorAction Stop 'https://cli.deplexo.com/install.ps1')))",
    shell: "PowerShell",
    destination: "%LOCALAPPDATA%\\Deplexo\\bin",
    script: "./install.ps1",
  },
};
const code = document.querySelector("#install-code");
const copy = document.querySelector("#copy");
const status = document.querySelector("#copy-status");
let copyTimer;
let selectedOS;
let betaVersion;
function selectOS(os) {
  selectedOS = os;
  const command = commands[os];
  document
    .querySelectorAll("[data-os]")
    .forEach((button) =>
      button.setAttribute("aria-pressed", String(button.dataset.os === os)),
    );
  code.textContent = command.code;
  document.querySelector("#shell-label").textContent = command.shell;
  document.querySelector("#install-detail").textContent = betaVersion
    ? `Installs the current release (${betaVersion}) to ${command.destination}. Run it again to update.`
    : `Installs the current release to ${command.destination}. Run it again to update.`;
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
async function loadRelease() {
  const response = await fetch("./latest-version", {
    signal: AbortSignal.timeout(6000),
  });
  if (!response.ok) return;
  const tag = (await response.text()).trim();
  if (
    !/^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-beta\.(0|[1-9][0-9]*))?$/.test(
      tag,
    )
  )
    return;
  const beta = tag.includes("-beta.");
  releaseStatus.textContent = `${tag} · ${beta ? "Beta release" : "Latest release"} ↗`;
  releaseStatus.href = `https://github.com/Deplexo/cli/releases/tag/${tag}`;
  if (beta) {
    betaVersion = tag;
    document.querySelector("#install-title").textContent = "Install the beta";
    document
      .querySelector(".install-note")
      .prepend(
        "This is a beta. Native testing is incomplete; check the release notes for your platform. ",
      );
    selectOS(selectedOS);
  }
}
loadRelease().catch(() => {
  /* Release links remain usable when the version lookup is unavailable. */
});
