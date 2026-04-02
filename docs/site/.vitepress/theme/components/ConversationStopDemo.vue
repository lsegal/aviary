<template>
	<div class="conversation-stop-demo" aria-label="Animated demo of parallel session work being stopped">
		<div class="demo-shell">
			<div class="demo-header">
				<div class="demo-agent">
					<span class="demo-agent-dot" />
					<span>assistant / main</span>
				</div>
				<div class="demo-status">parallel session activity</div>
			</div>

			<div class="demo-thread">
				<div class="bubble bubble-user bubble-user-a">
					Check the failing deploy and summarize the issue.
				</div>
				<div class="bubble bubble-user bubble-user-b">
					Also compare it to the last healthy release.
				</div>
				<div class="bubble bubble-assistant bubble-assistant-c">
					Starting both checks now.
				</div>

				<div class="typing-bubble">
					<div class="typing-row">
						<span class="typing-dot" />
						<span class="typing-dot" />
						<span class="typing-dot" />
					</div>
				</div>

				<div class="stop-row">
					<div class="bubble bubble-user bubble-stop">stop</div>
				</div>
			</div>
		</div>
	</div>
</template>

<style scoped>
.conversation-stop-demo {
	margin: 2rem 0;
}

.demo-shell {
	position: relative;
	overflow: hidden;
	border: 1px solid rgba(99, 62, 46, 0.18);
	border-radius: 26px;
	padding: 1rem 1rem 2rem;
	background:
		radial-gradient(circle at top right, rgba(214, 87, 31, 0.12), transparent 28%),
		linear-gradient(180deg, rgba(255, 251, 245, 0.92), rgba(245, 233, 221, 0.98));
	box-shadow:
		0 16px 30px -16px rgba(93, 53, 31, 0.28),
		inset 0 1px 0 rgba(255, 255, 255, 0.58);
}

.dark .demo-shell {
	border-color: rgba(255, 223, 196, 0.14);
	background:
		radial-gradient(circle at top right, rgba(214, 87, 31, 0.18), transparent 32%),
		linear-gradient(180deg, rgba(37, 25, 21, 0.96), rgba(22, 15, 13, 0.98));
	box-shadow:
		0 18px 36px -18px rgba(0, 0, 0, 0.6),
		inset 0 1px 0 rgba(255, 244, 232, 0.07);
}

.demo-header {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 1rem;
	padding: 0.1rem 0.15rem 0.95rem;
}

.demo-agent,
.demo-status {
	display: inline-flex;
	align-items: center;
	gap: 0.55rem;
	font-size: 0.78rem;
	font-weight: 700;
	letter-spacing: 0.04em;
	text-transform: uppercase;
	color: var(--vp-c-text-2);
}

.demo-agent-dot {
	width: 0.6rem;
	height: 0.6rem;
	border-radius: 999px;
	background: linear-gradient(180deg, #d54d21, #91240d);
	box-shadow: 0 0 0 4px rgba(213, 77, 33, 0.12);
}

.demo-thread {
	display: grid;
	gap: 0.72rem;
	height: 17rem;
	padding-bottom: 1.6rem;
	align-content: start;
	overflow: hidden;
	--typing-gap-shift: 2.55rem;
}

.bubble,
.typing-bubble,
.bubble-stop {
	opacity: 0;
	transform: translateY(10px);
	animation-duration: 10s;
	animation-iteration-count: infinite;
	animation-timing-function: ease;
	animation-fill-mode: both;
}

.bubble {
	max-width: min(100%, 26rem);
	padding: 0.78rem 0.95rem;
	border-radius: 1.15rem;
	font-size: 0.95rem;
	line-height: 1.45;
	box-shadow:
		0 10px 18px -14px rgba(93, 53, 31, 0.28),
		inset 0 1px 0 rgba(255, 255, 255, 0.4);
}

.bubble-user {
	justify-self: end;
	border-bottom-right-radius: 0.38rem;
	background: linear-gradient(180deg, rgba(218, 90, 41, 0.96), rgba(174, 51, 24, 0.98));
	color: #fff8f1;
}

.bubble-assistant {
	justify-self: start;
	border: 1px solid rgba(99, 62, 46, 0.13);
	border-bottom-left-radius: 0.38rem;
	background: rgba(255, 255, 255, 0.9);
	color: var(--vp-c-text-1);
}

.dark .bubble-assistant {
	border-color: rgba(255, 223, 196, 0.1);
	background: rgba(255, 244, 232, 0.08);
}

.typing-bubble {
	justify-self: start;
	display: inline-flex;
	align-items: center;
	padding: 0.5rem 0.78rem;
	border: 1px solid rgba(99, 62, 46, 0.08);
	border-radius: 999px;
	background: rgba(234, 230, 224, 0.9);
	box-shadow:
		0 10px 18px -14px rgba(93, 53, 31, 0.22),
		inset 0 1px 0 rgba(255, 255, 255, 0.42);
	will-change: transform, opacity;
}

.dark .typing-bubble {
	border-color: rgba(255, 223, 196, 0.08);
	background: rgba(255, 244, 232, 0.12);
}

.typing-row {
	display: inline-flex;
	align-items: center;
	gap: 0.3rem;
}

.typing-dot {
	width: 0.42rem;
	height: 0.42rem;
	border-radius: 999px;
	background: rgba(184, 58, 29, 0.76);
	animation: typing-bounce 1s ease-in-out infinite;
}

.typing-dot:nth-child(2) {
	animation-delay: 120ms;
}

.typing-dot:nth-child(3) {
	animation-delay: 240ms;
}

.stop-row {
	display: flex;
	justify-content: end;
	padding-top: 0.35rem;
}

.bubble-stop {
	max-width: none;
	padding: 0.78rem 0.95rem;
	font-family: inherit;
	font-size: 0.95rem;
	line-height: 1.45;
	font-weight: inherit;
	transform-origin: top right;
	will-change: transform, opacity;
}

.bubble-user-a {
	animation-name: message-a-cycle;
}

.bubble-user-b {
	animation-name: message-b-cycle;
}

.bubble-assistant-c {
	animation-name: message-c-cycle;
}

.typing-bubble {
	animation-name: typing-bubble-cycle;
}

.bubble-stop {
	animation-name: stop-message-cycle;
}

@keyframes message-a-cycle {
	0%,
	8% {
		opacity: 0;
		transform: translateY(10px);
	}
	12%,
	98.9% {
		opacity: 1;
		transform: translateY(0);
	}
	99%,
	100% {
		opacity: 0;
		transform: translateY(10px);
	}
}

@keyframes message-b-cycle {
	0%,
	13.5% {
		opacity: 0;
		transform: translateY(10px);
	}
	19%,
	98.9% {
		opacity: 1;
		transform: translateY(0);
	}
	99%,
	100% {
		opacity: 0;
		transform: translateY(10px);
	}
}

@keyframes message-c-cycle {
	0%,
	19% {
		opacity: 0;
		transform: translateY(10px);
	}
	24.5%,
	98.9% {
		opacity: 1;
		transform: translateY(0);
	}
	99%,
	100% {
		opacity: 0;
		transform: translateY(10px);
	}
}

@keyframes typing-bubble-cycle {
	0%,
	23% {
		opacity: 0;
		transform: translateY(10px);
	}
	28%,
	56% {
		opacity: 1;
		transform: translateY(0);
	}
	56.01%,
	100% {
		opacity: 0;
		transform: translateY(0);
	}
}

@keyframes stop-message-cycle {
	0%,
	34% {
		opacity: 0;
		transform: translateY(10px);
	}
	39% {
		opacity: 1;
		transform: translateY(0);
	}
	56% {
		opacity: 1;
		transform: translateY(0);
	}
	61% {
		opacity: 1;
		transform: translateY(calc(-1 * var(--typing-gap-shift)));
	}
	66%,
	98.9% {
		opacity: 1;
		transform: translateY(calc(-1 * var(--typing-gap-shift)));
	}
	99%,
	100% {
		opacity: 0;
		transform: translateY(10px);
	}
}

@keyframes typing-bounce {
	0%,
	100% {
		transform: translateY(0);
		opacity: 0.4;
	}
	50% {
		transform: translateY(-3px);
		opacity: 1;
	}
}

@media (prefers-reduced-motion: reduce) {
	.bubble,
	.typing-bubble,
	.bubble-stop,
	.typing-dot {
		animation: none !important;
		opacity: 1;
		transform: none;
	}

	.demo-thread {
		gap: 0.72rem;
		height: auto;
	}
}
</style>
