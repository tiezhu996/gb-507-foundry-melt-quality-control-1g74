import { useMemo } from 'react';
import { Typography } from '@mui/material';
import { EntityPage, type ColumnDefinition, type FieldDefinition } from '../components/EntityPage';
import { HeatStateBadge } from '../components/common/HeatStateBadge';
import { useHeatStore } from '../stores/heat';
import type { Heat } from '../types/domain';
import { HEAT_TRANSITIONS, type HeatState } from '../types/status';
import { formatDate } from '../utils/format';

const fields: readonly FieldDefinition[] = [
  { key: 'code', label: '炉次编码', required: true }, { key: 'name', label: '炉次名称', required: true },
  { key: 'furnaceCode', label: '炉台编码', required: true }, { key: 'alloyGrade', label: '合金牌号', required: true },
  { key: 'owner', label: '责任班组', required: true },
  { key: 'chargeWeightKg', label: '装料重量（kg）', type: 'number', required: true, min: 1, max: 500000, step: 1, defaultValue: 8000 },
  { key: 'targetTemperatureC', label: '目标温度（°C）', type: 'number', required: true, min: 500, max: 2200, step: 1, defaultValue: 1500 },
  { key: 'carbonMinPct', label: 'C 下限（%）', type: 'number', required: true, min: 0, max: 6, step: 0.001, defaultValue: 3.1 },
  { key: 'carbonMaxPct', label: 'C 上限（%）', type: 'number', required: true, min: 0, max: 6, step: 0.001, defaultValue: 3.5 },
  { key: 'siliconMinPct', label: 'Si 下限（%）', type: 'number', required: true, min: 0, max: 6, step: 0.001, defaultValue: 1.8 },
  { key: 'siliconMaxPct', label: 'Si 上限（%）', type: 'number', required: true, min: 0, max: 6, step: 0.001, defaultValue: 2.3 },
  { key: 'sulfurMaxPct', label: 'S 上限（%）', type: 'number', required: true, min: 0.001, max: 1, step: 0.001, defaultValue: 0.08 },
  { key: 'phosphorusMaxPct', label: 'P 上限（%）', type: 'number', required: true, min: 0.001, max: 1, step: 0.001, defaultValue: 0.12 },
  { key: 'startedAt', label: '开炉时间', type: 'datetime-local', required: true }, { key: 'evidence', label: 'MES 证据编号', required: true },
  { key: 'description', label: '备注', type: 'multiline' },
];

export default function HeatPage() {
  const items = useHeatStore((state) => state.items);
  const heatByCode = useMemo(() => new Map(items.map((item) => [item.code, item])), [items]);

  const columns: readonly ColumnDefinition<Heat>[] = useMemo(() => [
    { key: 'furnace', label: '炉台 / 牌号', minWidth: 140, render: (item) => <><Typography variant="body2" fontWeight={600}>{item.furnaceCode}</Typography><Typography variant="caption" color="text.secondary">{item.alloyGrade}</Typography></> },
    { key: 'process', label: '装料 / 温度', minWidth: 130, render: (item) => <><Typography variant="body2">{item.chargeWeightKg.toLocaleString()} kg</Typography><Typography variant="caption" color="text.secondary">{item.targetTemperatureC.toFixed(0)} °C</Typography></> },
    { key: 'chemistry', label: '冻结规格', minWidth: 220, render: (item) => <Typography variant="caption">C {item.carbonMinPct.toFixed(3)}-{item.carbonMaxPct.toFixed(3)} · Si {item.siliconMinPct.toFixed(3)}-{item.siliconMaxPct.toFixed(3)}<br />S ≤ {item.sulfurMaxPct.toFixed(3)} · P ≤ {item.phosphorusMaxPct.toFixed(3)}</Typography> },
    {
      key: 'remelt', label: '返炉关系 / 承接地', minWidth: 230,
      render: (item) => {
        if (item.originHeatCode) {
          return <Typography variant="caption" color="warning.main">返炉炉次 · 承接自原炉次<br /><b>{item.originHeatCode}</b><br />承接地炉台：<b>{item.furnaceCode}</b></Typography>;
        }
        if (item.returnedHeatCode) {
          // The承接地 is the furnace of the generated return heat, not the original furnace.
          const destination = heatByCode.get(item.returnedHeatCode)?.furnaceCode;
          return <Typography variant="caption" color="error.main">原炉次 · 已拒绝返炉<br />返炉炉次：<b>{item.returnedHeatCode}</b><br />承接地炉台：<b>{destination ?? '（请搜索该返炉炉次查看）'}</b></Typography>;
        }
        return <Typography variant="caption" color="text.secondary">普通炉次</Typography>;
      },
    },
    { key: 'owner', label: '责任 / 开炉', minWidth: 170, render: (item) => <><Typography variant="body2">{item.owner}</Typography><Typography variant="caption" color="text.secondary">{formatDate(item.startedAt)}</Typography></> },
  ], [heatByCode]);

  return <EntityPage
    path="heats" label="炉次工作台" description="按冻结成分规格推进熔炼、取样、质量待判与最终决定；返炉炉次与原炉次关系及承接地可直接回读。"
    useStore={useHeatStore} fields={fields} columns={columns} transitions={HEAT_TRANSITIONS}
    createRoles={['operator', 'reviewer', 'admin']} updateRoles={['operator', 'reviewer', 'admin']} transitionRoles={['operator', 'reviewer', 'admin']}
    editableStatuses={['charged']} deletableStatuses={['charged']}
    statusRender={(item) => <HeatStateBadge state={item.status as HeatState} />}
  />;
}
