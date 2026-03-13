import { ref } from "vue";
import { useMCP } from "./useMCP";

export function useStream() {
	const { callTool } = useMCP();
	const streaming = ref(false);
	const error = ref<string | null>(null);

	type StreamChunkType = "text" | "media" | "tool";

	async function streamAgent(
		agentName: string,
		message: string,
		onChunk: (chunk: string, type?: StreamChunkType) => void,
		sessionID = "",
		mediaURL?: string,
	): Promise<void> {
		streaming.value = true;
		error.value = null;

		try {
			let sawProgress = false;
			const toolArgs: Record<string, unknown> = {
				name: agentName,
				message,
				session_id: sessionID || undefined,
				session: "main",
				include_tool_progress: true,
			};
			if (mediaURL) toolArgs.media_url = mediaURL;

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
