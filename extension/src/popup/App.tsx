import React, { useEffect, useRef, useState } from 'react';
import type {
	StatusResponse,
	ForgetRequest,
	TagCount,
	IngestMode,
	ExtractIntent,
} from '../../../proto/types';

// Popup-only union: the radio set offers IngestMode (4 base modes) plus
// ExtractIntent (3 client-side strategies). Intents send `intent` to the SW
// instead of `mode` — daemon never sees them.
type PopupMode = IngestMode | ExtractIntent;
const INTENT_SET = new Set<PopupMode>([
	'selection',
	'yt_transcript',
	'yt_comments',
]);
import {
	DEFAULT_HOST,
	DEFAULT_PORT,
	DEFAULT_DWELL_MS,
	saveDaemonConfig,
	saveDwellThreshold,
} from '../background/native-bridge';
import { reindex, getReindexStatus } from '../background/daemon-client';

function formatAgo(ms: number): string {
	if (ms < 60_000) return `${Math.round(ms / 1000)}s`;
	if (ms < 3_600_000) return `${Math.round(ms / 60_000)}m`;
	return `${Math.round(ms / 3_600_000)}h`;
}

export default function App() {
	const [status, setStatus] = useState<StatusResponse | null>(null);
	const [error, setError] = useState<string | null>(null);
	const [loading, setLoading] = useState(true);
	const [connected, setConnected] = useState<boolean>(true);
	const [lastSeenTs, setLastSeenTs] = useState<number | null>(null);
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
	const [knownTags, setKnownTags] = useState<TagCount[]>([]);
	const [panelOpen, setPanelOpen] = useState(false);
	const [tagsCSV, setTagsCSV] = useState('');
	const [mode, setMode] = useState<PopupMode>('full_text');
	const [manualText, setManualText] = useState('');
	const [suggestLoading, setSuggestLoading] = useState(false);

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
				setConnected(false);
				return;
			}
			// res always carries connected/lastSeenTs (both success & error paths).
			if (typeof res?.connected === 'boolean')
				setConnected(res.connected);
			if (typeof res?.lastSeenTs === 'number' || res?.lastSeenTs === null)
				setLastSeenTs(res.lastSeenTs ?? null);
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
						if (Array.isArray(res?.tags)) {
							setTagsCSV((res.tags as string[]).join(', '));
						}
					},
				);
			}
		});
	}, []);

	useEffect(() => {
		chrome.runtime.sendMessage({ type: 'popup_list_tags' }, (res) => {
			if (chrome.runtime.lastError) return;
			if (Array.isArray(res?.tags)) setKnownTags(res.tags as TagCount[]);
		});
	}, []);

	// Listen for the SW broadcast that fires after /ingest resolves.
	// Replaces the transient "Indexing…" toast with the real outcome and
	// updates page-status state so the UI matches the daemon.
	useEffect(() => {
		const listener = (msg: {
			type?: string;
			ok?: boolean;
			error?: string;
		}) => {
			if (msg?.type !== 'ingest_complete') return;
			if (msg.ok) {
				flash('Indexed');
				setPageIndexed(true);
				setPageExists(true);
				chrome.runtime.sendMessage({ type: 'popup_list_tags' }, (r) => {
					if (chrome.runtime.lastError) return;
					if (Array.isArray(r?.tags))
						setKnownTags(r.tags as TagCount[]);
				});
			} else {
				flash(msg.error ?? 'Indexing failed', false);
			}
		};
		chrome.runtime.onMessage.addListener(listener);
		return () => chrome.runtime.onMessage.removeListener(listener);
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

	function parseTagsCSV(csv: string): string[] {
		return csv
			.split(',')
			.map((t) => t.trim())
			.filter((t) => t.length > 0);
	}

	function handleToggleIndexPanel() {
		setPanelOpen((v) => !v);
	}

	// CR-0003: merge generated tags into the existing CSV string, preserving
	// order and dropping case-insensitive duplicates.
	function mergeTagsCSV(existing: string, generated: string[]): string {
		const seen = new Set<string>();
		const out: string[] = [];
		for (const raw of existing.split(',')) {
			const t = raw.trim();
			if (!t) continue;
			const k = t.toLowerCase();
			if (seen.has(k)) continue;
			seen.add(k);
			out.push(t);
		}
		for (const t of generated) {
			const k = t.trim().toLowerCase();
			if (!k || seen.has(k)) continue;
			seen.add(k);
			out.push(t.trim());
		}
		return out.join(', ');
	}

	function handleSuggestTags() {
		setSuggestLoading(true);
		chrome.runtime.sendMessage({ type: 'popup_suggest_tags' }, (res) => {
			setSuggestLoading(false);
			if (chrome.runtime.lastError) {
				flash('Could not reach background', false);
				return;
			}
			if (res?.ok && Array.isArray(res.tags)) {
				const next = mergeTagsCSV(tagsCSV, res.tags as string[]);
				setTagsCSV(next);
				flash(`Suggested ${res.tags.length} tag(s)`);
			} else {
				flash(res?.error ?? 'Could not suggest tags', false);
			}
		});
	}

	function handleConfirmIndex() {
		const tags = parseTagsCSV(tagsCSV);
		const payload: {
			type: string;
			tags: string[];
			mode?: IngestMode;
			manualText?: string;
			intent?: ExtractIntent;
		} = { type: 'popup_force_index', tags };
		if (INTENT_SET.has(mode)) {
			payload.intent = mode as ExtractIntent;
		} else {
			payload.mode = mode as IngestMode;
			if (mode === 'manual') {
				const t = manualText.trim();
				if (!t) {
					flash('Type or paste the text to index', false);
					return;
				}
				payload.manualText = t;
			}
		}
		chrome.runtime.sendMessage(payload, (res) => {
			if (chrome.runtime.lastError) return;
			if (res?.ok) {
				// Extraction succeeded — but /ingest still hasn't been called.
				// Show a non-expiring "Indexing…" toast; the `ingest_complete`
				// listener (mounted in useEffect) will replace it with the real
				// success/error result from the daemon.
				setMsg({ text: 'Indexing…', ok: true });
				setPanelOpen(false);
				setManualText('');
			} else {
				flash(res?.error ?? 'Could not index page', false);
			}
		});
	}

	function handleIgnoreDomain() {
		chrome.runtime.sendMessage({ type: 'popup_ignore_domain' }, (res) => {
			if (chrome.runtime.lastError) return;
			if (res?.ok) flash(`${res.domain} ignored`);
			else flash(res?.error ?? 'Could not ignore domain', false);
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
			<style>{`
				input::placeholder, textarea::placeholder {
					color: #c0c4cc;
					font-style: italic;
					opacity: 1;
				}
			`}</style>
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
								placeholder="e.g. example.com or full URL"
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

			{/* Daemon offline banner — shown when SW has no recent healthy contact. */}
			{!loading && !connected && (
				<div
					style={{
						fontSize: '12px',
						color: '#991b1b',
						background: '#fef2f2',
						border: '1px solid #fecaca',
						borderRadius: '4px',
						padding: '6px 8px',
					}}
				>
					<strong>Daemon offline.</strong> Start <code>vbmd</code>
					{lastSeenTs
						? ` — last seen ${formatAgo(Date.now() - lastSeenTs)} ago.`
						: '.'}
				</div>
			)}

			{/* Status */}
			{loading && (
				<div style={{ fontSize: '12px', color: '#9ca3af' }}>
					Connecting...
				</div>
			)}
			{error && connected && <div style={s.errorBox}>{error}</div>}
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
					{status.daemonPort && connected && (
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

			{/* Index this site — single entry point; click expands the options panel */}
			<button style={s.indexBtn} onClick={handleToggleIndexPanel}>
				{panelOpen ? 'Cancel' : 'Index this site now'}
			</button>

			{panelOpen && (
				<div
					style={{
						display: 'flex',
						flexDirection: 'column',
						gap: '10px',
						padding: '10px',
						border: '1px solid #e5e7eb',
						borderRadius: '6px',
						background: '#fafafa',
					}}
				>
					<div>
						<div style={s.label}>Tags</div>
						<div
							style={{
								display: 'flex',
								gap: '5px',
								alignItems: 'stretch',
							}}
						>
							<input
								style={{ ...s.input, flex: 1 }}
								type="text"
								list="vbm-known-tags"
								placeholder="e.g. ai, work, read-later"
								value={tagsCSV}
								onChange={(e) => setTagsCSV(e.target.value)}
							/>
							<button
								type="button"
								title="Suggest up to 3 tags via LLM"
								onClick={handleSuggestTags}
								disabled={suggestLoading}
								style={{
									flexShrink: 0,
									width: '32px',
									padding: '0',
									border: '1px solid #d1d5db',
									borderRadius: '4px',
									background: suggestLoading
										? '#f3f4f6'
										: '#fff',
									cursor: suggestLoading
										? 'default'
										: 'pointer',
									fontSize: '14px',
									lineHeight: 1,
								}}
							>
								{suggestLoading ? '…' : '✨'}
							</button>
						</div>
						<datalist id="vbm-known-tags">
							{knownTags.map((t) => (
								<option key={t.tag} value={t.tag}>
									{t.count}
								</option>
							))}
						</datalist>
					</div>

					<div>
						<div style={s.label}>How to index</div>
						<div
							style={{
								display: 'flex',
								flexDirection: 'column',
								gap: '4px',
								fontSize: '12px',
								color: '#374151',
							}}
						>
							{(() => {
								const baseRadios: Array<[PopupMode, string]> = [
									[
										'full_text',
										'Index every chunk (full text)',
									],
									[
										'llm_summary',
										'Summarize via LLM, then index',
									],
									['selection', 'Only the selected text'],
									['manual', 'Type the text manually'],
									['meta_only', 'Only meta tags + title'],
								];
								const isYTWatch = (() => {
									if (!currentTabUrl) return false;
									try {
										const u = new URL(currentTabUrl);
										return (
											u.hostname.endsWith(
												'youtube.com',
											) && u.pathname === '/watch'
										);
									} catch {
										return false;
									}
								})();
								const ytRadios: Array<[PopupMode, string]> = [
									[
										'yt_transcript',
										'YouTube — transcribe (CC)',
									],
									[
										'yt_comments',
										'YouTube — comments (visible)',
									],
								];
								return (
									<>
										{baseRadios.map(([val, label]) => (
											<label
												key={val}
												style={{
													display: 'flex',
													alignItems: 'center',
													gap: '6px',
													cursor: 'pointer',
												}}
											>
												<input
													type="radio"
													name="vbm-mode"
													value={val}
													checked={mode === val}
													onChange={() =>
														setMode(val)
													}
												/>
												<span>{label}</span>
											</label>
										))}
										{isYTWatch && (
											<>
												<div
													style={{
														fontSize: '10px',
														color: '#9ca3af',
														marginTop: '4px',
														textTransform:
															'uppercase',
														letterSpacing: '.04em',
													}}
												>
													— YouTube
												</div>
												{ytRadios.map(
													([val, label]) => (
														<label
															key={val}
															style={{
																display: 'flex',
																alignItems:
																	'center',
																gap: '6px',
																cursor: 'pointer',
															}}
														>
															<input
																type="radio"
																name="vbm-mode"
																value={val}
																checked={
																	mode === val
																}
																onChange={() =>
																	setMode(val)
																}
															/>
															<span>{label}</span>
														</label>
													),
												)}
											</>
										)}
									</>
								);
							})()}
						</div>
					</div>

					{mode === 'manual' && (
						<div>
							<div style={s.label}>Manual text</div>
							<textarea
								style={{
									...s.input,
									width: '100%',
									minHeight: '90px',
									fontFamily: 'inherit',
									resize: 'vertical',
								}}
								placeholder="Paste or type the content to index…"
								value={manualText}
								onChange={(e) => setManualText(e.target.value)}
							/>
						</div>
					)}

					<button
						style={{
							...s.indexBtn,
							background: '#111',
							color: '#fff',
							border: 'none',
							fontWeight: 500,
						}}
						onClick={handleConfirmIndex}
					>
						Confirm
					</button>
				</div>
			)}

			{/* Don't track this site — subordinate, destructive-ish */}
			<button
				onClick={handleIgnoreDomain}
				style={{
					background: 'none',
					border: 'none',
					cursor: 'pointer',
					fontSize: '11px',
					color: '#9ca3af',
					padding: '2px 0',
					textAlign: 'left' as const,
					textDecoration: 'underline',
					textDecorationStyle: 'dotted' as const,
				}}
			>
				Don't track this site
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
