-- name: GetUsageByDate :many
-- Aggregates usage data from agent messages.
-- Returns one row per (date, model) pair.
SELECT
  date(m.created_at) as date,
  c.model,
  COUNT(*) as message_count,
  COALESCE(SUM(json_extract(m.usage_data, '$.input_tokens')), 0) as total_input_tokens,
  COALESCE(SUM(json_extract(m.usage_data, '$.output_tokens')), 0) as total_output_tokens,
  COALESCE(SUM(json_extract(m.usage_data, '$.cost_usd')), 0) as total_cost_usd
FROM messages m
JOIN conversations c ON m.conversation_id = c.conversation_id
WHERE m.type = 'agent'
  AND m.usage_data IS NOT NULL
  AND m.created_at >= ?
GROUP BY date(m.created_at), c.model
ORDER BY date(m.created_at) DESC;

-- name: GetUsageByConversation :many
-- Aggregates usage per conversation for a date range.
SELECT
  c.conversation_id,
  c.slug,
  c.model,
  COUNT(*) as message_count,
  COALESCE(SUM(json_extract(m.usage_data, '$.input_tokens')), 0) as total_input_tokens,
  COALESCE(SUM(json_extract(m.usage_data, '$.output_tokens')), 0) as total_output_tokens,
  COALESCE(SUM(json_extract(m.usage_data, '$.cost_usd')), 0) as total_cost_usd
FROM messages m
JOIN conversations c ON m.conversation_id = c.conversation_id
WHERE m.type = 'agent'
  AND m.usage_data IS NOT NULL
  AND m.created_at >= ?
GROUP BY c.conversation_id
ORDER BY total_cost_usd DESC;
