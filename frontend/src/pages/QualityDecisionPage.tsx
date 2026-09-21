import { useEffect, useState } from 'react';
import AutorenewIcon from '@mui/icons-material/Autorenew';
import { IconButton, Tooltip, Typography } from '@mui/material';
import { request } from '../api/client';
import { EntityPage, type ColumnDefinition, type FieldDefinition } from '../components/EntityPage';
import { ChemistryTable } from '../components/common/ChemistryTable';
import { HeatStateBadge } from '../components/common/HeatStateBadge';
import { RemeltDialog } from '../components/common/RemeltDialog';
import { useQualityDecisionStore } from '../stores/quality-decision';
import { useAuth } from '../hooks/useAuth';
import type { ChemicalSample, QualityDecision } from '../types/domain';
import { DECISION_TRANSITIONS, type HeatState } from '../types/status';
import { formatDate } from '../utils/format';

const fields: readonly FieldDefinition[] = [
  { key: 'code', label: '决定编码', required: true }, { key: 'name', label: '决定名称', required: true },
  { key: 'heatCode', label: '炉次编码', required: true }, { key: 'sampleCode', label: '样本编码', required: true },
  { key: 'reason', label: '判定依据', type: 'multiline', required: true }, { key: 'conditions', label: '附加条件', type: 'multiline' },
  { key: 'decidedAt', label: '判定时间', type: 'datetime-local', required: true }, { key: 'evidence', label: '签发证据编号', required: true },
  { key: 'description', label: '备注', type: 'multiline' },
];

const columns: readonly ColumnDefinition<QualityDecision>[] = [
  { key: 'relation', label: '炉次 / 样本', minWidth: 150, render: (item) => <><Typography variant="body2" fontWeight={600}>{item.heatCode}</Typography><Typography variant="caption" color="text.secondary">{item.sampleCode}</Typography></> },
  { key: 'reviewer', label: '复核人 / 时间', minWidth: 170, render: (item) => <><Typography variant="body2">{item.reviewer}</Typography><Typography variant="caption" color="text.secondary">{formatDate(item.decidedAt)}</Typography></> },
  { key: 'reason', label: '判定依据', minWidth: 220, render: (item) => <Typography variant="body2" className="cell-wrap">{item.reason}</Typography> },
  {
    key: 'remeltLink', label: '返炉关联结果', minWidth: 190,
    render: (item) => item.status === 'remelt'
      ? <dl className="remelt-link">
        <dt>返炉炉次</dt><dd>{item.remeltHeatCode || '—'}</dd>
        <dt>承接地炉台</dt><dd>{item.remeltFurnaceCode || '—'}</dd>
        <dt>承接时间</dt><dd>{item.remeltedAt ? formatDate(item.remeltedAt) : '—'}</dd>
      </dl>
      : <Typography variant="caption" color="text.secondary">{item.status === 'draft' ? '尚未签发' : '无返炉关联'}</Typography>,
  },
];

function derivedHeatState(status: string): HeatState {
  if (status === 'accept') return 'accepted';
  if (status === 'remelt' || status === 'scrap') return 'rejected';
  return 'hold';
}

export default function QualityDecisionPage() {
  const [samples, setSamples] = useState<ChemicalSample[]>([]);
  const [remelting, setRemelting] = useState<QualityDecision | null>(null);
  const { items, meta, load } = useQualityDecisionStore();
  const { hasRole } = useAuth();
  const canRemelt = hasRole('reviewer', 'admin');

  useEffect(() => {
    request<ChemicalSample[]>('/samples?page=1&pageSize=8').then((result) => setSamples(result.data)).catch(() => setSamples([]));
  }, []);

  // Remelt has a dedicated closed-loop action (linked heat + compliant
  // furnace), so it is intentionally removed from the generic transition menu.
  const transitions: Record<string, readonly string[]> = {
    ...DECISION_TRANSITIONS,
    draft: DECISION_TRANSITIONS.draft.filter((value) => value !== 'remelt'),
  };

  function refresh() {
    void load('decisions', '', meta.page, meta.pageSize);
  }

  return <>
    <EntityPage
      path="decisions" label="质量判定" description="基于同炉次终态化验结果签发接收、返炉或报废决定；返炉将在合规炉台生成关联返炉炉次。"
      useStore={useQualityDecisionStore} fields={fields} columns={columns} transitions={transitions}
      createRoles={['reviewer', 'admin']} updateRoles={['reviewer', 'admin']} transitionRoles={['reviewer', 'admin']}
      editableStatuses={['draft']} deletableStatuses={['draft']}
      statusRender={(item) => <span className="decision-status"><span>{item.status}</span><HeatStateBadge state={derivedHeatState(item.status)} /></span>}
      extraRowActions={(item) => canRemelt && item.status === 'draft'
        ? <Tooltip title="判定返炉（生成关联返炉炉次）"><span><IconButton size="small" color="secondary" onClick={() => setRemelting(item)}><AutorenewIcon fontSize="small" /></IconButton></span></Tooltip>
        : null}
      footer={<ChemistryTable records={samples} title="签发关联化验明细" />}
    />
    {remelting && <RemeltDialog
      decision={items.find((item) => item.id === remelting.id) || remelting}
      onClose={() => setRemelting(null)}
      onSuccess={() => { setRemelting(null); refresh(); }}
    />}
  </>;
}
