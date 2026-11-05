// Runs the rem SessionStart hook with a Claude Code-style JSON payload on stdin.
// additionalContext reaches the model with the next prompt.
import { spawn } from "node:child_process";

const command = '${REM_HOME:-$HOME/.local/share/rem}/hooks/session-start.sh';
const timeoutMs = 15000;

function runHook(cwd: string): Promise<string> {
	return new Promise((resolve) => {
		const child = spawn("/bin/sh", ["-c", command], { cwd, stdio: ["pipe", "pipe", "ignore"] });
		let stdout = "";
		const timer = setTimeout(() => child.kill("SIGKILL"), timeoutMs);
		child.stdout.on("data", (chunk) => (stdout += chunk));
		child.on("error", () => {
			clearTimeout(timer);
			resolve("");
		});
		child.on("close", (code) => {
			clearTimeout(timer);
			resolve(code === 0 ? stdout : "");
		});
		child.stdin.on("error", () => {});
		child.stdin.end(JSON.stringify({ hook_event_name: "SessionStart", cwd, agent: "pi" }));
	});
}

function additionalContext(stdout: string): string {
	try {
		const context = JSON.parse(stdout)?.hookSpecificOutput?.additionalContext;
		return typeof context === "string" ? context : "";
	} catch {
		return "";
	}
}

export default function (pi: any) {
	let context = "";
	pi.on("session_start", async (_event: unknown, ctx: any) => {
		context = additionalContext(await runHook(ctx?.cwd ?? process.cwd()));
	});
	pi.on("before_agent_start", () => {
		if (!context) return undefined;
		const content = context;
		context = "";
		return { message: { customType: "rem-hook", content, display: false } };
	});
}
