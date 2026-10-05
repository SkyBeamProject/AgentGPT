# Beam Agent

Beam Agent is a local autonomous agent you can install and run on your own computer. It plans a goal, uses a few tools, and keeps the result in a folder you choose.

It does not use the AgentGPT Docker stack, MySQL database, or Next.js app. Any OpenAI-compatible model server works, including OpenAI, Ollama, and LM Studio.

The Windows build produces three artifacts:

| File | What it is |
| --- | --- |
| `beam-agent-windows-amd64.exe` | Portable program. No installer. |
| `BeamAgentSetup.exe` | Per-user installer. Adds a Start menu shortcut and an entry in Apps. |
| `BeamAgent.msi` | Per-machine MSI for Program Files. |

A signed MSIX package has to be built on Windows because it needs the Windows SDK. The script is `packaging/windows/build-msix.ps1`.

## Install on Windows

From a checkout:

```bash
cd local-agent
./packaging/build.sh
```

The installers are written to `local-agent/dist/`. GitHub Actions also uploads them as the `beam-agent-windows` artifact.

`BeamAgentSetup.exe` does not ask for an administrator password. It installs to `%LOCALAPPDATA%\Programs\Beam Agent` and can start the app on the last wizard page. Windows SmartScreen may warn about an unsigned installer; choose **More info**, then **Run anyway**.

The MSI installs for all users into `C:\Program Files\Beam Agent` and adds a Start menu shortcut.

To build the MSIX on a Windows machine that has the Windows SDK:

```powershell
copy dist\beam-agent-windows-amd64.exe packaging\windows\beam-agent.exe
cd packaging\windows
.\build-msix.ps1
```

The script creates a certificate for the current user, signs the package, and installs it.

## Use it

Opening the program starts a page at `http://127.0.0.1:8787/` (or the next free port). The page is bound to this computer only.

1. Choose **OpenAI** or **Ollama on this computer**.
2. For OpenAI, paste an API key and save. For Ollama, start Ollama first; no key is required.
3. Name an agent and write a persona. That persona is the custom agent.
4. Write a goal and press **Run**. **Practice run** does the same loop without calling a model, which is a good way to confirm the install.

Ctrl+Enter also starts a run.

Terminal use:

```bash
beam-agent config preset ollama
beam-agent agents add --name Researcher --persona "Research the goal and write a short markdown brief in the workspace."
beam-agent run --goal "Summarize the files already in the workspace"
beam-agent run --goal "Leave a note" --mock
```

Settings are stored in `%APPDATA%\beam-agent\config.json` on Windows and `~/.config/beam-agent/config.json` elsewhere. The API key file is readable only by your user. `OPENAI_API_KEY` and `BEAM_API_KEY` are used when no key is saved.

The default workspace is `Documents\Beam Agent`, or `~/Beam Agent` when Documents does not exist. Finished runs are copied to `beam-runs/` inside that folder. If the workspace contains `BEAM.md` or `AGENTS.md`, those instructions are added to the agent.

## Tools

The agent can list, read, write, and delete files inside the workspace, and it can fetch a public `http` or `https` page. Shell commands stay off until you enable them. Enabling shell lets the model run programs on this computer; a short blocklist refuses formatting a disk, shutting the machine down, and deleting the filesystem root. The workspace itself cannot be a system directory.

## Build requirements

Go 1.22 or newer. The Windows packages also need NSIS, WiX via `wixl`, and a MinGW `windres` if you want the `.exe` icon. `packaging/build.sh` runs the tests, builds both binaries, and writes the installers.

## License

Beam Agent is part of this repository and is licensed under the GNU General Public License version 3. The installers include `LICENSE.txt` and `SOURCE.txt`.
