import React, { useEffect, useRef, useState } from 'react';
import type { StatusResponse, ForgetRequest } from '../../../proto/types';
import {
	DEFAULT_HOST,
	DEFAULT_PORT,
	DEFAULT_DWELL_MS,
	saveDaemonConfig,
	saveDwellThreshold,
} from '../background/native-bridge';
import { reindex, getReindexStatus } from '../background/daemon-client';

export default function App() {
	const [status, setStatus] = useState<StatusResponse | null>(null);
	const [error, setError] = useState<string | null>(null);
	const [loading, setLoading] = useState(true);
	const [forgetValue, setForgetValue] = useState('');
	const [forgetType, setForgetType] = useState<'url' | 'domain'>('url');
	const [msg, setMsg] = useState<{ text: string; ok: boolean } | null>(null);
	const [showSettings, setShowSettings] = useState(false);
	const [host, setHost] = useState(DEFAULT_HOST);
	const [port, setPort] = useState(String(DEFAULT_PORT));
	const [dwellSecs, setDwellSecs] = useState(String(DEFAULT_DWELL_MS / 1000));
	const [pageExists, setPageExists] = useState<boolean | null>(null);
	const [pageIndexed, setPageIndexed] = useState<boolean | null>(null);
	const [currentTabUrl, setCurrentTabUrl] = useState<string | null>(null);
	const [reindexProgress, setReindexProgress] = useState<{
		running: boolean;
		done: number;
		total: number;
	} | null>(null);
	const reindexPollRef = useRef<ReturnType<typeof setInterval> | null>(null);

	useEffect(() => {
		chrome.storage.local.get(
			['vbmHost', 'vbmPort', 'vbmDwellMs'],
			(result) => {
				if (result.vbmHost) setHost(result.vbmHost as string);
				if (result.vbmPort) setPort(String(result.vbmPort));
				if (result.vbmDwellMs)
					setDwellSecs(String((result.vbmDwellMs as number) / 1000));
			},
		);
	}, []);

	useEffect(() => {
		chrome.runtime.sendMessage({ type: 'popup_status' }, (res) => {
			setLoading(false);
			if (chrome.runtime.lastError) {
				setError(chrome.runtime.lastError.message ?? 'Unknown error');
				return;
			}
			if (res?.error) setError(res.error);
			else setStatus(res as StatusResponse);
		});
		chrome.tabs.query({ active: true, currentWindow: true }, (tabs) => {
			const url = tabs[0]?.url ?? null;
			setCurrentTabUrl(url);
			if (
				url &&
				!url.startsWith('chrome://') &&
				!url.startsWith('chrome-extension://')
			) {
				chrome.runtime.sendMessage(
					{ type: 'popup_page_status' },
					(res) => {
						if (chrome.runtime.lastError) return;
						setPageExists(res?.exists ?? false);
						setPageIndexed(res?.indexed ?? false);
					},
				);
			}
		});
	}, []);

	function flash(text: string, ok = true) {
		setMsg({ text, ok });
		setTimeout(() => setMsg(null), 2500);
	}

	function handleToggleCapture() {
		const nextEnabled = !(status?.captureEnabled ?? true);
		chrome.runtime.sendMessage(
			{ type: 'popup_set_capture', enabled: nextEnabled },
			(res) => {
				if (chrome.runtime.lastError) return;
				if (res?.ok)
					setStatus((s) =>
						s ? { ...s, captureEnabled: res.captureEnabled } : s,
					);
			},
		);
	}

	function handleForceIndex() {
		chrome.runtime.sendMessage({ type: 'popup_force_index' }, (res) => {
			if (chrome.runtime.lastError) return;
			if (res?.ok) flash('Page queued for indexing');
			else flash(res?.error ?? 'Could not index page', false);
		});
	}

	function handleRemovePage() {
		if (!currentTabUrl) return;
		const req: ForgetRequest = { type: 'url', value: currentTabUrl };
		chrome.runtime.sendMessage({ type: 'popup_forget', req }, (res) => {
			if (chrome.runtime.lastError) return;
			if (res?.ok) {
				flash('Removed');
				setPageExists(false);
				setPageIndexed(false);
			} else {
				flash(res?.error ?? 'Failed', false);
			}
		});
	}

	function handleForget() {
		if (!forgetValue.trim()) return;
		const req: ForgetRequest = {
			type: forgetType,
			value: forgetValue.trim(),
		};
		chrome.runtime.sendMessage({ type: 'popup_forget', req }, (res) => {
			if (chrome.runtime.lastError) return;
			if (res?.ok) {
				flash('Forgotten');
				setForgetValue('');
			} else {
				flash(res?.error ?? 'Failed', false);
			}
		});
	}

	function handleReembed() {
		reindex()
			.then(({ started }) => {
				if (!started) {
					flash('Re-embed already running', false);
					return;
				}
				setReindexProgress({ running: true, done: 0, total: 0 });
				reindexPollRef.current = setInterval(async () => {
					try {
						const st = await getReindexStatus();
						setReindexProgress(st);
						if (!st.running) {
							clearInterval(reindexPollRef.current!);
							reindexPollRef.current = null;
							flash(`Re-embedded ${st.done} chunks`);
							setReindexProgress(null);
						}
					} catch {
						clearInterval(reindexPollRef.current!);
						reindexPollRef.current = null;
						setReindexProgress(null);
					}
				}, 1500);
			})
			.catch(() => flash('Re-embed failed', false));
	}

	function handleSaveConfig() {
		const p = parseInt(port, 10);
		if (!host.trim() || isNaN(p) || p < 1 || p > 65535) return;
		const d = parseFloat(dwellSecs);
		const dwellMs =
			isNaN(d) || d < 1 ? DEFAULT_DWELL_MS : Math.round(d * 1000);
		Promise.all([
			saveDaemonConfig({ host: host.trim(), port: p }),
			saveDwellThreshold(dwellMs),
		]).then(() => {
			flash('Saved');
			setShowSettings(false);
		});
	}

	const paused = !(status?.captureEnabled ?? true);

	// ── styles ────────────────────────────────────────────────────────────────

	const s = {
		container: {
			width: '300px',
			padding: '12px 14px',
			display: 'flex',
			flexDirection: 'column' as const,
			gap: '10px',
			boxSizing: 'border-box' as const,
			fontFamily:
				"-apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif",
		},
		header: {
			display: 'flex',
			alignItems: 'center',
			justifyContent: 'space-between',
		},
		title: {
			fontWeight: 600,
			fontSize: '14px',
			margin: 0,
			color: '#111',
		},
		headerActions: {
			display: 'flex',
			gap: '6px',
			alignItems: 'center',
		},
		pauseBtn: {
			fontSize: '11px',
			padding: '3px 8px',
			border: '1px solid #d1d5db',
			borderRadius: '4px',
			cursor: 'pointer',
			background: paused ? '#fef2f2' : '#f9fafb',
			color: paused ? '#dc2626' : '#374151',
		},
		gearBtn: {
			fontSize: '13px',
			padding: '2px 6px',
			border: '1px solid #e5e7eb',
			borderRadius: '4px',
			cursor: 'pointer',
			background: showSettings ? '#f3f4f6' : 'transparent',
			color: '#6b7280',
			lineHeight: 1,
		},
		divider: {
			height: '1px',
			background: '#f3f4f6',
			margin: '0 -14px',
		},
		stat: {
			fontSize: '13px',
			color: '#374151',
			display: 'flex',
			gap: '12px',
		},
		muted: { color: '#9ca3af' },
		errorBox: {
			fontSize: '12px',
			color: '#991b1b',
			background: '#fef2f2',
			border: '1px solid #fecaca',
			borderRadius: '4px',
			padding: '6px 8px',
		},
		flashBox: (ok: boolean): React.CSSProperties => ({
			fontSize: '12px',
			color: ok ? '#166534' : '#991b1b',
			background: ok ? '#f0fdf4' : '#fef2f2',
			border: `1px solid ${ok ? '#bbf7d0' : '#fecaca'}`,
			borderRadius: '4px',
			padding: '5px 8px',
		}),
		indexBtn: {
			width: '100%',
			padding: '7px',
			border: '1px solid #d1d5db',
			borderRadius: '5px',
			background: '#f9fafb',
			color: '#374151',
			fontSize: '12px',
			cursor: 'pointer',
			textAlign: 'center' as const,
		},
		label: {
			fontSize: '11px',
			fontWeight: 600,
			color: '#6b7280',
			marginBottom: '5px',
		},
		row: { display: 'flex', gap: '5px' },
		input: {
			flex: 1,
			minWidth: 0,
			fontSize: '12px',
			padding: '5px 8px',
			border: '1px solid #d1d5db',
			borderRadius: '4px',
			outline: 'none',
			boxSizing: 'border-box' as const,
		},
		select: {
			flexShrink: 0,
			fontSize: '12px',
			padding: '5px 6px',
			border: '1px solid #d1d5db',
			borderRadius: '4px',
			background: '#fff',
		},
		btnRed: {
			flexShrink: 0,
			fontSize: '12px',
			padding: '5px 10px',
			border: 'none',
			borderRadius: '4px',
			background: '#ef4444',
			color: '#fff',
			cursor: 'pointer',
		},
		btnGray: {
			flexShrink: 0,
			fontSize: '12px',
			padding: '5px 10px',
			border: 'none',
			borderRadius: '4px',
			background: '#6b7280',
			color: '#fff',
			cursor: 'pointer',
		},
		portInput: {
			width: '64px',
			flexShrink: 0,
			fontSize: '12px',
			padding: '5px 8px',
			border: '1px solid #d1d5db',
			borderRadius: '4px',
			outline: 'none',
			boxSizing: 'border-box' as const,
		},
		uiLink: {
			fontSize: '12px',
			color: '#6366f1',
			background: 'none',
			border: 'none',
			cursor: 'pointer',
			padding: 0,
			textDecoration: 'none',
		},
		footer: {
			fontSize: '11px',
			color: '#9ca3af',
			borderTop: '1px solid #f3f4f6',
			paddingTop: '8px',
		},
	};

	return (
		<div style={s.container}>
			{/* Header */}
			<div style={s.header}>
				<p style={s.title}>Vector Bookmark</p>
				<div style={s.headerActions}>
					<button style={s.pauseBtn} onClick={handleToggleCapture}>
						{paused ? 'Resume' : 'Pause'}
					</button>
					<button
						style={s.gearBtn}
						onClick={() => setShowSettings((v) => !v)}
						title="Daemon settings"
					>
						⚙
					</button>
				</div>
			</div>

			{/* Settings panel */}
			{showSettings && (
				<div
					style={{
						display: 'flex',
						flexDirection: 'column',
						gap: '8px',
					}}
				>
					<div>
						<div style={s.label}>Daemon address</div>
						<div style={s.row}>
							<input
								style={s.input}
								type="text"
								value={host}
								onChange={(e) => setHost(e.target.value)}
								onKeyDown={(e) =>
									e.key === 'Enter' && handleSaveConfig()
								}
							/>
							<input
								style={s.portInput}
								type="number"
								value={port}
								onChange={(e) => setPort(e.target.value)}
								onKeyDown={(e) =>
									e.key === 'Enter' && handleSaveConfig()
								}
							/>
							<button
								style={s.btnGray}
								onClick={handleSaveConfig}
							>
								Save
							</button>
						</div>
					</div>
					<div>
						<div style={s.label}>Dwell time (seconds)</div>
						<div style={s.row}>
							<input
								style={s.input}
								type="number"
								min="1"
								step="1"
								value={dwellSecs}
								onChange={(e) => setDwellSecs(e.target.value)}
								onKeyDown={(e) =>
									e.key === 'Enter' && handleSaveConfig()
								}
							/>
							<button
								style={s.btnGray}
								onClick={handleSaveConfig}
							>
								Save
							</button>
						</div>
					</div>
					{(status?.indexed ?? 0) > 0 && (
						<button
							style={{ ...s.indexBtn, color: '#6366f1' }}
							onClick={handleReembed}
							disabled={reindexProgress?.running}
						>
							{reindexProgress?.running
								? `Re-embedding… ${reindexProgress.done}/${reindexProgress.total}`
								: 'Re-embed pages (semantic search)'}
						</button>
					)}
					<div>
						<div style={s.label}>Forget</div>
						<div style={s.row}>
							<input
								style={s.input}
								type="text"
								placeholder="URL or domain..."
								value={forgetValue}
								onChange={(e) => setForgetValue(e.target.value)}
								onKeyDown={(e) =>
									e.key === 'Enter' && handleForget()
								}
							/>
							<select
								style={s.select}
								value={forgetType}
								onChange={(e) =>
									setForgetType(
										e.target.value as 'url' | 'domain',
									)
								}
							>
								<option value="url">URL</option>
								<option value="domain">Domain</option>
							</select>
							<button style={s.btnRed} onClick={handleForget}>
								Forget
							</button>
						</div>
					</div>
				</div>
			)}

			<div style={s.divider} />

			{/* Status */}
			{loading && (
				<div style={{ fontSize: '12px', color: '#9ca3af' }}>
					Connecting...
				</div>
			)}
			{error && <div style={s.errorBox}>{error}</div>}
			{status && (
				<div style={s.stat}>
					<span>
						<strong>{status.visited}</strong> visited
					</span>
					<span>
						<strong>{status.indexed}</strong> indexed
					</span>
					{status.pending > 0 && (
						<span>
							<strong>{status.pending}</strong> pending
						</span>
					)}
					{status.daemonPort && (
						<a
							href={`http://${host}:${status.daemonPort}/ui`}
							target="_blank"
							rel="noreferrer"
							style={s.uiLink}
						>
							Open UI ↗
						</a>
					)}
				</div>
			)}

			{/* Semantic search warning */}
			{status &&
				status.embedderVersion === 'stub-v0' &&
				status.indexed > 0 && (
					<div
						style={{
							fontSize: '11px',
							color: '#92400e',
							background: '#fffbeb',
							border: '1px solid #fde68a',
							borderRadius: '4px',
							padding: '5px 8px',
						}}
					>
						Semantic search off — set <code>VBM_EMBED_API_KEY</code>{' '}
						and restart daemon. See docs/OPERATIONS.md §4.
					</div>
				)}

			{/* Flash message */}
			{msg && <div style={s.flashBox(msg.ok)}>{msg.text}</div>}

			{/* Index this page */}
			<button style={s.indexBtn} onClick={handleForceIndex}>
				Index this page now
			</button>

			{/* Page status indicator */}
			{pageIndexed === true && (
				<div
					style={{
						display: 'flex',
						alignItems: 'center',
						justifyContent: 'space-between',
						fontSize: '12px',
						color: '#166534',
						background: '#f0fdf4',
						border: '1px solid #bbf7d0',
						borderRadius: '4px',
						padding: '5px 8px',
					}}
				>
					<span>✓ This page is indexed</span>
					<button
						onClick={handleRemovePage}
						style={{
							background: 'none',
							border: 'none',
							cursor: 'pointer',
							fontSize: '12px',
							color: '#991b1b',
							padding: 0,
						}}
					>
						Remove
					</button>
				</div>
			)}
			{pageExists === true && pageIndexed === false && (
				<div
					style={{
						fontSize: '12px',
						color: '#1e40af',
						background: '#eff6ff',
						border: '1px solid #bfdbfe',
						borderRadius: '4px',
						padding: '5px 8px',
					}}
				>
					● Visited — not yet indexed
				</div>
			)}

			{/* Footer */}
			<div style={s.footer}>@recall in address bar to search</div>
		</div>
	);
}
