import { ref } from "vue";
import { useMCP } from "./useMCP";

export type StreamChunkType = "text" | "media" | "tool";

/** One message to run on an agent with useStream's streamAgent. */
export interface StreamAgentRequest {
	agentName: string;
	message: string;
	/** The session to continue by ID; empty opens `session` by name. */
	sessionID?: string;
	mediaURLs?: string[];
	project?: string;
	/** The session's name; "main" when unset. */
	session?: string;
	retry?: boolean;
}

export function useStream() {
	const { callTool } = useMCP();
	const streaming = ref(false);
	const error = ref<string | null>(null);

	async function streamAgent(
		request: StreamAgentRequest,
		onChunk: (chunk: string, type?: StreamChunkType) => void,
	): Promise<void> {
		const {
			agentName,
			message,
			sessionID = "",
			mediaURLs = [],
			project,
			session = "main",
			retry = false,
		} = request;
		streaming.value = true;
		error.value = null;

		try {
			let sawProgress = false;
			const toolArgs: Record<string, unknown> = {
				name: agentName,
				message,
				session_id: sessionID || undefined,
				session,
				include_tool_progress: true,
			};
			if (mediaURLs.length) toolArgs.media_urls = mediaURLs;
			if (project) toolArgs.project = project;
			if (retry) toolArgs.retry = true;

			const text = await callTool("agent_run", toolArgs, {
				onProgress: (chunk) => {
					sawProgress = true;
					if (chunk.startsWith("[media]")) {
						onChunk(chunk.slice("[media]".length), "media");
					} else if (chunk.startsWith("[tool]")) {
						onChunk(chunk.slice("[tool]".length), "tool");
					} else {
						onChunk(chunk, "text");
					}
				},
			});
			if (!sawProgress && text) {
				onChunk(text, "text");
			}
		} catch (e) {
			error.value = e instanceof Error ? e.message : String(e);
			throw e;
		} finally {
			streaming.value = false;
		}
	}

	return { streaming, error, streamAgent };
}
