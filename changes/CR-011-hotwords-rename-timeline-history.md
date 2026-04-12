# CR-011 — Renomear Timeline→HotWords + nova aba Timeline (histórico cronológico)

**Data:** 2026-04-12
**Solicitante:** user
**App Version:** 0.1.0
**Status:** implemented ✓
**Urgência:** low
**Domínios afetados:** Daemon (store, routes, ui)

---

## Descrição da mudança

1. **Renomear** a aba "Timeline" existente (keywords por período) para **"HotWords"** — o nome descreve melhor o conteúdo.
2. **Nova aba "Timeline"**: exibe o histórico cronológico de navegação — cada página indexada com data, domínio, título e palavras-chave extraídas. Inclui gráfico de barras SVG com atividade por dia.

## Motivação

"Timeline" era ambíguo; "HotWords" descreve o que a aba faz. A nova Timeline preenche a necessidade de ver cronologicamente o que foi lido, com contexto de palavras-chave por página — diferente do HotWords que agrega frequência no período.

## Comportamento desejado

- Aba "HotWords": idêntica à Timeline anterior.
- Aba "Timeline": navegação por semana/mês, gráfico de barras SVG (atividade por dia), lista de páginas com data, domínio (⭐ se starred), título linkado, top 5 palavras-chave.
- Endpoint `GET /history?from=<ms>&to=<ms>&limit=100` no daemon.

---

## Critério de aceite

- [x] Aba "HotWords" exibe o conteúdo anterior de Timeline sem regressão
- [x] Aba "Timeline" exibe gráfico de barras + lista cronológica de páginas
- [x] Palavras-chave extraídas por página (top 5)
- [x] Estrela (⭐) exibida para páginas com `star_rank=1`
- [x] Navegação semana/mês funciona corretamente

---

*CR-011 — gerado em 2026-04-12 — status: implemented*
