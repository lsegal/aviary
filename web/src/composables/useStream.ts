import { ref } from "vue";
import { useMCP } from "./useMCP";

export function useStream() {
	const { callTool } = useMCP();
	const streaming = ref(false);
	const error = ref<string | null>(null);

	async function streamAgent(
		agentName: string,
		message: string,
		onChunk: (chunk: string, isMedia?: boolean) => void,
		session = "main",
		mediaURL?: string,
	): Promise<void> {
		streaming.value = true;
		error.value = null;

		try {
			let sawProgress = false;
			const toolArgs: Record<string, string> = { name: agentName, message, session };
			if (mediaURL) toolArgs.media_url = mediaURL;

			const text = await callTool(
				"agent_run",
				toolArgs,
				{
					onProgress: (chunk) => {
						sawProgress = true;
						if (chunk.startsWith("[media]")) {
							onChunk(chunk.slice("[media]".length), true);
						} else {
							onChunk(chunk, false);
						}
					},
				},
			);
			if (!sawProgress && text) {
				onChunk(text, false);
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
