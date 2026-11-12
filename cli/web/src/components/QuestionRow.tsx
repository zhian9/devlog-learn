import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Check, RotateCcw } from 'lucide-react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import type { QuestionEntry } from '../types'
import { Badge, ErrorBox } from './ui'

export function QuestionRow({
  entry,
  showDoc = false,
}: {
  entry: QuestionEntry
  showDoc?: boolean
}) {
  const queryClient = useQueryClient()
  const [note, setNote] = useState('')
  const resolved = entry.status === 'resolved'

  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: ['questions'] })
    queryClient.invalidateQueries({ queryKey: ['status'] })
    queryClient.invalidateQueries({ queryKey: ['doc', entry.doc_id] })
  }

  const resolve = useMutation({
    mutationFn: () => api.resolveQuestion(entry.id, note),
    onSuccess: refresh,
  })
  const reopen = useMutation({
    mutationFn: () => api.reopenQuestion(entry.id),
    onSuccess: refresh,
  })

  return (
    <div className={`question-row${resolved ? ' resolved' : ''}`}>
      <div className="question-main">
        <p className="question-text">{entry.question}</p>
        <div className="question-meta">
          {showDoc ? (
            <Link to={`/docs/${entry.doc_id}`} className="text-link">
              {entry.doc_title || entry.doc_id}
            </Link>
          ) : (
            <span>{entry.doc_title || entry.doc_id}</span>
          )}
          <span>·</span>
          <span>{entry.project}</span>
          <Badge tone={resolved ? 'green' : 'amber'}>
            {resolved ? '已解决' : '待解决'}
          </Badge>
        </div>
        {resolved && entry.note && <div className="question-note">备注：{entry.note}</div>}
      </div>

      {resolved ? (
        <button
          className="button ghost small"
          onClick={() => reopen.mutate()}
          disabled={reopen.isPending}
        >
          <RotateCcw size={13} /> 重新打开
        </button>
      ) : (
        <div className="question-actions">
          <input
            value={note}
            onChange={(event) => setNote(event.target.value)}
            placeholder="解决备注（可选）"
            aria-label="解决备注"
          />
          <button
            className="button primary small"
            onClick={() => resolve.mutate()}
            disabled={resolve.isPending}
          >
            <Check size={13} /> 标记解决
          </button>
        </div>
      )}

      {(resolve.error || reopen.error) && (
        <div className="question-error">
          <ErrorBox error={resolve.error ?? reopen.error} />
        </div>
      )}
    </div>
  )
}
