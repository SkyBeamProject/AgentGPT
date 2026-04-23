# Overview

AgentGPT lets you configure and deploy autonomous AI agents from the browser. Name a custom AI, give it a goal, and it plans tasks, executes them, and learns from the results.

Architecture:

- **Frontend** (`next/`) — Next.js 13 + TypeScript + TailwindCSS + Zod, auth via NextAuth + Prisma.
- **Backend** (`platform/`) — FastAPI + SQLModel + Pydantic.
- **Database** — MySQL (Planetscale).
- **LLM tooling** — LangChain.
- **Bootstrapping** — create-t3-app + FastAPI-template.

See `README.md` for prerequisites (Node.js, Docker, OpenAI API key) and the `setup.sh` / `setup.bat` guided install.
