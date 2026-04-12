#!/bin/bash

# Always source the real GVM shell integration so `gvm use` is available.
if [ ! -s "$HOME/.gvm/scripts/gvm" ]; then
    echo "[x] Error: script GVM not found at $HOME/.gvm/scripts/gvm"
    exit 1
fi

# shellcheck source=/dev/null
source "$HOME/.gvm/scripts/gvm"

if [ "$(type -t gvm)" != "function" ]; then
    echo "[x] Error: gvm shell function failed to load."
    exit 1
fi

if [ -z "$GVM_ROOT" ]; then
    echo "[x] Error: GVM_ROOT not found. Make sure GVM is set up correctly."
    exit 1
fi

# 1. Detecting active Go version from GVM
GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
echo "[*] Detected Go version: $GO_VERSION"

if [ -z "$GO_VERSION" ]; then
    echo "[x] Error: GVM Go not detected. Run 'gvm use' first."
    exit 1
fi

# 1a. Explicitly activate the same Go version via gvm.
echo "[*] Test command: gvm use go$GO_VERSION"
if ! gvm use "go$GO_VERSION" >/dev/null; then
    echo "[x] Error: failed to run 'gvm use go$GO_VERSION'."
    exit 1
fi

GVM_GOROOT="$GVM_ROOT/gos/go$GO_VERSION"
GVM_GOPATH="$GVM_ROOT/pkgsets/go$GO_VERSION/global"

# 2. Ensure the .vscode folder exists
mkdir -p .vscode

# 2a. Generate a custom rcfile for the VS Code terminal.
cat <<EOF > .vscode/gvm-terminal-rc.sh
#!/bin/bash

if [ -f "\$HOME/.bashrc" ]; then
    # shellcheck source=/dev/null
    source "\$HOME/.bashrc"
fi

unset GOROOT

if [ -s "\$HOME/.gvm/scripts/gvm" ]; then
    # shellcheck source=/dev/null
    source "\$HOME/.gvm/scripts/gvm"
    gvm use go$GO_VERSION >/dev/null 2>&1
fi
EOF

chmod +x .vscode/gvm-terminal-rc.sh

# 3. Auto-install crucial tools if missing (To avoid 'Cannot find tool' errors)
if [ ! -f "$GVM_GOPATH/bin/gopls" ]; then
    echo "[!] Tools missing. Installing gopls & dlv for $GO_VERSION..."
    go install golang.org/x/tools/gopls@latest
    go install github.com/go-delve/delve/cmd/dlv@latest
fi

echo "[+] Generating Configs for Go $GO_VERSION..."

# 4. Generate settings.json (Consistent: GOROOT, GOPATH, and PATH all synchronized)
cat <<EOF > .vscode/settings.json
{
    "go.goroot": "$GVM_GOROOT",
    "go.gopath": "$GVM_GOPATH",
    "go.toolsGopath": "$GVM_GOPATH",
    "go.alternateTools": {
        "go": "$GVM_GOROOT/bin/go",
        "gopls": "$GVM_GOPATH/bin/gopls"
    },
    "go.toolsEnvVars": {
        "GOROOT": "$GVM_GOROOT",
        "GOPATH": "$GVM_GOPATH",
        "PATH": "$GVM_GOPATH/bin:$GVM_GOROOT/bin:\${env:PATH}"
    },
    "terminal.integrated.env.linux": {
        "GOROOT": "$GVM_GOROOT",
        "GOPATH": "$GVM_GOPATH",
        "PATH": "$GVM_GOPATH/bin:$GVM_GOROOT/bin:\${env:PATH}"
    },
    "terminal.integrated.profiles.linux": {
        "bash": {
            "path": "/bin/bash",
            "args": [
                "--rcfile",
                "\${workspaceFolder}/.vscode/gvm-terminal-rc.sh",
                "-i"
            ]
        }
    },
    "terminal.integrated.defaultProfile.linux": "bash",
    "terminal.integrated.automationProfile.linux": {
        "path": "/bin/bash",
        "args": [
            "--rcfile",
            "\${workspaceFolder}/.vscode/gvm-terminal-rc.sh",
            "-i"
        ]
    },
    "go.useLanguageServer": true,
    "go.inferGopath": true
}
EOF

# 5. Generate launch.json (So that F5/Debugger runs with the same Go version)
cat <<EOF > .vscode/launch.json
{
    "version": "0.2.0",
    "configurations": [
        {
            "name": "Debug (Go $GO_VERSION)",
            "type": "go",
            "request": "launch",
            "mode": "auto",
            "program": "\${fileDirname}",
            "env": {
                "GOROOT": "$GVM_GOROOT",
                "GOPATH": "$GVM_GOPATH",
                "PATH": "$GVM_GOPATH/bin:$GVM_GOROOT/bin:\${env:PATH}"
            }
        }
    ]
}
EOF

echo "[+] Setup complete! VS Code is now configured for Go $GO_VERSION."
