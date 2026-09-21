import { useEffect, useMemo, useState } from 'react';
import {
  Alert, Box, Button, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle,
  Divider, MenuItem, TextField, Typography,
} from '@mui/material';
import { listRemeltFurnaces, submitRemelt } from '../../api/quality-decision';
import { ApiError, request } from '../../api/client';
import type { Heat, QualityDecision, RemeltFurnaceOption, RemeltInput } from '../../types/domain';

interface RemeltDialogProps {
  decision: QualityDecision;
  onClose: () => void;
  onSuccess: () => void;
}

interface FormState {
  furnaceCode: string;
  returnHeatCode: string;
  returnHeatName: string;
  owner: string;
  evidence: string;
  reason: string;
}

function initialForm(decision: QualityDecision): FormState {
  const suffix = `${decision.heatCode}-R`;
  return {
    furnaceCode: '',
    returnHeatCode: suffix.slice(0, 64),
    returnHeatName: `返炉承接炉次（源 ${decision.heatCode}）`,
    owner: '',
    evidence: '',
    reason: '',
  };
}

// RemeltDialog performs the "判定返炉" closed loop. The furnace selector only
// offers currently compliant furnaces (available, grade/capacity/temperature),
// and the whole operation is one atomic submission on the backend.
export function RemeltDialog({ decision, onClose, onSuccess }: RemeltDialogProps) {
  const [form, setForm] = useState<FormState>(() => initialForm(decision));
  const [furnaces, setFurnaces] = useState<RemeltFurnaceOption[]>([]);
  const [origin, setOrigin] = useState<Heat | null>(null);
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    let active = true;
    setLoading(true);
    Promise.all([
      listRemeltFurnaces(decision.id),
      request<Heat[]>(`/heats?page=1&pageSize=100&search=${encodeURIComponent(decision.heatCode)}`),
    ]).then(([furnaceResult, heatResult]) => {
      if (!active) return;
      setFurnaces(furnaceResult.data);
      setOrigin(heatResult.data.find((heat) => heat.code === decision.heatCode) || null);
      if (furnaceResult.data.length === 1) {
        setForm((current) => ({ ...current, furnaceCode: furnaceResult.data[0].code }));
      }
    }).catch((err: unknown) => {
      if (active) setError(err instanceof ApiError ? err.message : '加载可用炉台失败');
    }).finally(() => {
      if (active) setLoading(false);
    });
    return () => { active = false; };
  }, [decision.id, decision.heatCode]);

  const selectedFurnace = useMemo(
    () => furnaces.find((furnace) => furnace.code === form.furnaceCode) || null,
    [furnaces, form.furnaceCode],
  );

  const valid = form.furnaceCode !== '' &&
    form.returnHeatCode.trim().length >= 2 &&
    form.returnHeatName.trim().length >= 2 &&
    form.reason.trim().length >= 3 &&
    form.evidence.trim().length >= 3;

  function update(key: keyof FormState, value: string) {
    setForm((current) => ({ ...current, [key]: value }));
  }

  async function submit() {
    if (!valid) return;
    setSubmitting(true);
    setError('');
    const payload: RemeltInput = {
      expectedVersion: decision.version,
      reason: form.reason.trim(),
      furnaceCode: form.furnaceCode.trim(),
      returnHeatCode: form.returnHeatCode.trim().toUpperCase(),
      returnHeatName: form.returnHeatName.trim(),
      owner: form.owner.trim(),
      evidence: form.evidence.trim(),
    };
    try {
      await submitRemelt(decision.id, payload);
      onSuccess();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '返炉承接提交失败');
      setSubmitting(false);
    }
  }

  return <Dialog open onClose={submitting ? undefined : onClose} maxWidth="sm" fullWidth>
    <DialogTitle>判定返炉承接 · {decision.code}</DialogTitle>
    <DialogContent dividers className="dialog-stack">
      <Alert severity="info" variant="outlined">
        一次提交将：把原炉次 <strong>{decision.heatCode}</strong> 置为已拒绝、在合规炉台生成关联返炉炉次、继承原成分规格，并把炉台推进至装料。
      </Alert>

      {loading && <Box className="table-loading" sx={{ position: 'static', py: 2 }}><CircularProgress size={26} /></Box>}

      {!loading && furnaces.length === 0 && <Alert severity="warning" variant="outlined">
        当前没有满足原牌号、装料量和目标温度的可用炉台，无法承接返炉。请先释放或维护合适炉台后重试。
      </Alert>}

      {origin && <Box className="remelt-origin">
        <Typography variant="subtitle2">原炉次冻结规格</Typography>
        <Typography variant="body2" color="text.secondary">
          {origin.alloyGrade} · {origin.chargeWeightKg.toLocaleString()} kg · 目标 {origin.targetTemperatureC.toFixed(0)} °C
        </Typography>
        <Typography variant="caption" color="text.secondary">
          C {origin.carbonMinPct.toFixed(3)}-{origin.carbonMaxPct.toFixed(3)} ·
          Si {origin.siliconMinPct.toFixed(3)}-{origin.siliconMaxPct.toFixed(3)} ·
          S ≤ {origin.sulfurMaxPct.toFixed(3)} · P ≤ {origin.phosphorusMaxPct.toFixed(3)}
        </Typography>
      </Box>}

      <TextField
        select label="承接炉台（仅显示当前可用且合规）" value={form.furnaceCode} required fullWidth
        disabled={loading || furnaces.length === 0}
        onChange={(event) => update('furnaceCode', event.target.value)}
      >
        {furnaces.map((furnace) => <MenuItem key={furnace.code} value={furnace.code}>
          {furnace.code} · {furnace.name}（{furnace.plantArea} · {furnace.capacityTonnes.toFixed(1)}t · ≤{furnace.maxTemperatureC.toFixed(0)}°C）
        </MenuItem>)}
      </TextField>

      {selectedFurnace && <Typography variant="caption" color="text.secondary">
        承接地：{selectedFurnace.plantArea} / {selectedFurnace.furnaceType} / 责任班组 {selectedFurnace.operator}
      </Typography>}

      <Divider />
      <TextField label="返炉炉次编码" value={form.returnHeatCode} required fullWidth inputProps={{ maxLength: 64 }}
        onChange={(event) => update('returnHeatCode', event.target.value.toUpperCase())} />
      <TextField label="返炉炉次名称" value={form.returnHeatName} required fullWidth
        onChange={(event) => update('returnHeatName', event.target.value)} />
      <TextField label="责任班组（留空继承原班组）" value={form.owner} fullWidth
        onChange={(event) => update('owner', event.target.value)} />
      <TextField label="新熔炼循环证据编号" value={form.evidence} required fullWidth
        onChange={(event) => update('evidence', event.target.value)} />
      <TextField label="返炉原因" value={form.reason} required fullWidth multiline minRows={3}
        onChange={(event) => update('reason', event.target.value)} />

      {error && <Alert severity="error" variant="outlined">{error}</Alert>}
    </DialogContent>
    <DialogActions>
      <Button onClick={onClose} disabled={submitting}>取消</Button>
      <Button variant="contained" onClick={() => void submit()} disabled={loading || submitting || !valid}>
        {submitting ? '提交中…' : '确认返炉承接'}
      </Button>
    </DialogActions>
  </Dialog>;
}
