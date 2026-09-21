import { useEffect, useState } from 'react';
import { IconButton, Tooltip, Typography } from '@mui/material';
import AutorenewIcon from '@mui/icons-material/Autorenew';
import { request } from '../api/client';
import { EntityPage, type ColumnDefinition, type FieldDefinition } from '../components/EntityPage';
import { ChemistryTable } from '../components/common/ChemistryTable';
import { HeatStateBadge } from '../components/common/HeatStateBadge';
import { RemeltHandoverDialog } from '../components/common/RemeltHandoverDialog';
import { useQualityDecisionStore } from '../stores/quality-decision';
import type { ChemicalSample, QualityDecision, RemeltHandoverResult } from '../types/domain';
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
  {
    key: 'remelt', label: '返炉关联结果', minWidth: 240,
    render: (item) => item.status === 'remelt' && item.remeltHeatCode
      ? <Typography variant="caption">原炉次 <b>{item.heatCode}</b> 已拒绝<br />返炉炉次 <b>{item.remeltHeatCode}</b>（可在炉次页搜索回读）<br />承接地：<b>{item.remeltFurnaceCode}</b></Typography>
      : <Typography variant="caption" color="text.secondary">{item.status === 'remelt' ? '关联结果缺失' : '—'}</Typography>,
  },
  { key: 'reviewer', label: '复核人 / 时间', minWidth: 170, render: (item) => <><Typography variant="body2">{item.reviewer}</Typography><Typography variant="caption" color="text.secondary">{formatDate(item.decidedAt)}</Typography></> },
  { key: 'reason', label: '判定依据', minWidth: 220, render: (item) => <Typography variant="body2" className="cell-wrap">{item.reason}</Typography> },
  { key: 'evidence', label: '签发证据', minWidth: 150, render: (item) => item.evidence },
];

function derivedHeatState(status: string): HeatState {
  if (status === 'accept') return 'accepted';
  if (status === 'remelt' || status === 'scrap') return 'rejected';
  return 'hold';
}

export default function QualityDecisionPage() {
  const [samples, setSamples] = useState<ChemicalSample[]>([]);
  const [remeltTarget, setRemeltTarget] = useState<QualityDecision | null>(null);
  const [notice, setNotice] = useState('');
  const store = useQualityDecisionStore();
  useEffect(() => {
    request<ChemicalSample[]>('/samples?page=1&pageSize=8').then((result) => setSamples(result.data)).catch(() => setSamples([]));
  }, []);

  function handleRemeltSubmitted(result: RemeltHandoverResult) {
    setRemeltTarget(null);
    setNotice(`返炉闭环完成：原炉次 ${result.originHeat.code} 已拒绝，返炉炉次 ${result.returnHeat.code} 已在 ${result.furnace.code} 生成并进入装料状态。`);
    void store.load('decisions', '', store.meta.page, store.meta.pageSize);
  }

  return <>
    <EntityPage
      path="decisions" label="质量判定" description="基于同炉次终态化验结果签发接收、返炉或报废决定；返炉需一次提交完成炉台承接闭环。"
      useStore={useQualityDecisionStore} fields={fields} columns={columns} transitions={DECISION_TRANSITIONS}
      createRoles={['reviewer', 'admin']} updateRoles={['reviewer', 'admin']} transitionRoles={['reviewer', 'admin']}
      editableStatuses={['draft']} deletableStatuses={['draft']} hiddenTargets={['remelt']}
      statusRender={(item) => <span className="decision-status"><span>{item.status}</span><HeatStateBadge state={derivedHeatState(item.status)} /></span>}
      rowActions={(item) => item.status === 'draft'
        ? <Tooltip title="返炉承接闭环（选择合规炉台并生成返炉炉次）"><span>
          <IconButton size="small" color="warning" disabled={store.loading} onClick={() => { setNotice(''); setRemeltTarget(item); }}>
            <AutorenewIcon fontSize="small" />
          </IconButton>
        </span></Tooltip>
        : null}
      footer={<>{notice && <Typography variant="body2" color="success.main" sx={{ mt: 2 }}>{notice}</Typography>}<ChemistryTable records={samples} title="签发关联化验明细" /></>}
    />
    <RemeltHandoverDialog decision={remeltTarget} onClose={() => setRemeltTarget(null)} onSubmitted={handleRemeltSubmitted} />
  </>;
}
