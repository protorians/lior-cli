#!/usr/bin/env node

"use strict";

const { execSync } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");
const https = require("https");
const http = require("http");

const REPO = "protorians/liorian-cli";
const BINARY_NAME = "liorian";

function getOsToken() {
  const platform = os.platform();
  const arch = os.arch();
  switch (platform) {
    case "darwin":
      return `Macintosh; ${arch === "arm64" ? "" : "Intel "}Mac OS X ${execSync("sw_vers -productVersion").toString().trim()}`;
    case "linux":
      return `X11; Linux ${arch === "x64" ? "x86_64" : arch === "arm64" ? "aarch64" : arch}`;
    case "win32":
      return `Windows NT 10.0; Win64; ${arch === "arm64" ? "ARM64" : "x64"}`;
    default:
      return `${platform}; ${arch}`;
  }
}

function getUserAgent() {
  const pkg = require("../package.json");
  // `Node/<version>` is the engine token of Node's core `http`/`https`
  // package; `Senteints/<version>` is the CLI.
  return `Protorians/5.0 (${getOsToken()}) Node/${process.version.slice(1)} Senteints/${pkg.version}`;
}

const PLATFORM_MAP = {
  darwin: { amd64: "darwin_amd64", arm64: "darwin_arm64" },
  linux: { amd64: "linux_amd64", arm64: "linux_arm64" },
  win32: { amd64: "windows_amd64" },
};

const ARCHIVE_EXT = {
  darwin: "tar.gz",
  linux: "tar.gz",
  win32: "zip",
};

function getVersion() {
  const pkg = require("../package.json");
  return `v${pkg.version}`;
}

function getPlatform() {
  const platform = os.platform();
  const arch = os.arch();

  if (!PLATFORM_MAP[platform]) {
    throw new Error(`Unsupported platform: ${platform}`);
  }
  if (!PLATFORM_MAP[platform][arch]) {
    throw new Error(`Unsupported architecture: ${platform}/${arch}`);
  }

  return {
    platform,
    arch,
    triple: PLATFORM_MAP[platform][arch],
    ext: ARCHIVE_EXT[platform],
  };
}

function getDownloadUrl(version, platform) {
  const archiveVersion = version.replace(/^v/, "");
  const archiveName = `${BINARY_NAME}-cli_${archiveVersion}_${platform.triple}.${platform.ext}`;
  return `https://github.com/${REPO}/releases/download/${version}/${archiveName}`;
}

function download(url) {
  return new Promise((resolve, reject) => {
    const mod = url.startsWith("https") ? https : http;
    mod
      .get(url, { headers: { "User-Agent": getUserAgent() } }, (res) => {
        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          return download(res.headers.location).then(resolve, reject);
        }
        if (res.statusCode !== 200) {
          reject(new Error(`Download failed with status ${res.statusCode}: ${url}`));
          return;
        }
        const chunks = [];
        res.on("data", (chunk) => chunks.push(chunk));
        res.on("end", () => resolve(Buffer.concat(chunks)));
        res.on("error", reject);
      })
      .on("error", reject);
  });
}

async function extractArchive(archivePath, destDir, platform) {
  if (platform.ext === "zip") {
    execSync(`powershell -Command "Expand-Archive -Path '${archivePath}' -DestinationPath '${destDir}' -Force"`, {
      stdio: "inherit",
    });
  } else {
    execSync(`tar -xzf "${archivePath}" -C "${destDir}"`, { stdio: "inherit" });
  }
}

async function main() {
  const version = getVersion();
  const platform = getPlatform();
  const binDir = path.join(__dirname, "..", "bin");
  const tmpDir = path.join(os.tmpdir(), `liorian-install-${Date.now()}`);

  console.log(`Installing @liorian/cli ${version} for ${platform.triple}...`);

  try {
    fs.mkdirSync(tmpDir, { recursive: true });
    fs.mkdirSync(binDir, { recursive: true });

    const url = getDownloadUrl(version, platform);
    console.log(`Downloading from ${url}...`);

    const data = await download(url);
    const archivePath = path.join(tmpDir, `release.${platform.ext}`);
    fs.writeFileSync(archivePath, data);

    console.log("Extracting...");
    await extractArchive(archivePath, tmpDir, platform);

    const binaryExt = platform.platform === "win32" ? ".exe" : "";
    const extractedBinary = path.join(tmpDir, `${BINARY_NAME}${binaryExt}`);
    const targetBinary = path.join(binDir, `${BINARY_NAME}${binaryExt}`);

    if (fs.existsSync(extractedBinary)) {
      fs.copyFileSync(extractedBinary, targetBinary);
    } else {
      // Try to find the binary in subdirectories
      const files = fs.readdirSync(tmpDir, { recursive: true });
      const found = files.find(
        (f) => f.toString().endsWith(`${BINARY_NAME}${binaryExt}`) || f.toString().endsWith(`${BINARY_NAME}.exe`)
      );
      if (found) {
        fs.copyFileSync(path.join(tmpDir, found.toString()), targetBinary);
      } else {
        throw new Error(`Binary ${BINARY_NAME} not found in archive`);
      }
    }

    if (platform.platform !== "win32") {
      fs.chmodSync(targetBinary, 0o755);
    }

    console.log(`@liorian/cli ${version} installed successfully.`);
  } catch (err) {
    console.error(`Failed to install @liorian/cli: ${err.message}`);
    console.error("");
    console.error("You can install the binary manually from:");
    console.error(`  https://github.com/${REPO}/releases`);
    process.exit(1);
  } finally {
    // Cleanup
    try {
      fs.rmSync(tmpDir, { recursive: true, force: true });
    } catch {}
  }
}

main();
