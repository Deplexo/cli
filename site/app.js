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
  code.textContent = betaVersion
    ? os === "windows"
      ? `${command.code} -Version '${betaVersion}'`
      : command.code.replace("| sh", `| DEPLEXO_VERSION=${betaVersion} sh`)
    : command.code;
  document.querySelector("#shell-label").textContent = command.shell;
  document.querySelector("#install-detail").textContent = betaVersion
    ? `Installs beta ${betaVersion} to ${command.destination}. Change the pinned version to update.`
    : `Installs the latest stable release to ${command.destination}. Run it again to update.`;
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
  const response = await fetch(
    "https://api.github.com/repos/Deplexo/cli/releases/latest",
    { signal: AbortSignal.timeout(6000) },
  );
  if (response.ok) {
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
    return;
  }
  if (response.status !== 404) return;
  releaseStatus.textContent = "No stable release published yet ↗";
  const betas = await fetch(
    "https://api.github.com/repos/Deplexo/cli/releases?per_page=20",
    { signal: AbortSignal.timeout(6000) },
  );
  if (!betas.ok) return;
  const releases = await betas.json();
  if (!Array.isArray(releases)) return;
  const beta = releases.find(
    (release) =>
      !release.draft &&
      release.prerelease &&
      /^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-beta\.(0|[1-9][0-9]*)$/.test(
        release.tag_name,
      ),
  );
  if (!beta) return;
  betaVersion = beta.tag_name;
  releaseStatus.textContent = `${betaVersion} · Beta release ↗`;
  releaseStatus.href = `https://github.com/Deplexo/cli/releases/tag/${betaVersion}`;
  document.querySelector("#install-title").textContent = "Install the beta";
  document
    .querySelector(".install-note")
    .prepend(
      "This is a beta. Native testing is incomplete; check the release notes for your platform. ",
    );
  selectOS(selectedOS);
}
loadRelease().catch(() => {
  /* Release links remain usable when the API is unavailable. */
});
