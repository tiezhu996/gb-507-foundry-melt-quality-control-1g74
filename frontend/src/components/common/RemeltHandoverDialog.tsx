import { useEffect, useMemo, useState } from 'react';
import {
  Alert, Box, Button, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle,
  Divider, MenuItem, TextField, Typography,
} from '@mui/material';
import AutorenewIcon from '@mui/icons-material/Autorenew';
import { listRemeltFurnaces, submitRemeltHandover } from '../../api/remelt';
import type { QualityDecision, RemeltFurnaceOption, RemeltHandoverResult } from '../../types/domain';

interface Props {
  decision: QualityDecision | null;
  onClose: () => void;
  onSubmitted: (result: RemeltHandoverResult) => void;
}

interface FormState {
  furnaceCode: string;
  returnHeatCode: string;
  returnHeatName: string;
  reason: string;
  evidence: string;
}

const EMPTY_FORM: FormState = { furnaceCode: '', returnHeatCode: '', returnHeatName: '', reason: '', evidence: '' };

export function RemeltHandoverDialog({ decision, onClose, onSubmitted }: Props) {
  const [options, setOptions] = useState<RemeltFurnaceOption[]>([]);
  const [loadingOptions, setLoadingOptions] = useState(false);
  const [optionsError, setOptionsError] = useState('');
  const [form, setForm] = useState<FormState>(EMPTY_FORM);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState('');

  useEffect(() => {
    if (!decision) return undefined;
    setOptions([]);
    setOptionsError('');
    setSubmitError('');
    setForm({ ...EMPTY_FORM, returnHeatCode: `${decision.heatCode}-R`, reason: '成分不满足冻结规格，安排返炉重熔' });
    setLoadingOptions(true);
    let active = true;
    listRemeltFurnaces(decision.heatCode)
      .then((result) => {
        if (!active) return;
        setOptions(result.data);
        const first = result.data.find((option) => option.eligible);
        setForm((current) => ({ ...current, furnaceCode: first?.code || '' }));
      })
      .catch((error: unknown) => {
        if (active) setOptionsError(error instanceof Error ? error.message : String(error));
      })
      .finally(() => active && setLoadingOptions(false));
    return () => { active = false; };
  }, [decision]);

  const eligibleOptions = useMemo(() => options.filter((option) => option.eligible), [options]);
  const ineligibleOptions = useMemo(() => options.filter((option) => !option.eligible), [options]);
  const selectedOption = useMemo(
    () => options.find((option) => option.code === form.furnaceCode),
    [options, form.furnaceCode],
  );
  const formValid = form.furnaceCode.trim() !== '' && form.returnHeatCode.trim().length >= 2 &&
    form.returnHeatName.trim().length >= 2 && form.reason.trim().length >= 3 && form.evidence.trim().length >= 3;

  async function submit() {
    if (!decision || !formValid) return;
    setSubmitting(true);
    setSubmitError('');
    try {
      const result = await submitRemeltHandover(decision.id, {
        expectedVersion: decision.version,
        furnaceCode: form.furnaceCode.trim(),
        returnHeatCode: form.returnHeatCode.trim().toUpperCase(),
        returnHeatName: form.returnHeatName.trim(),
        reason: form.reason.trim(),
        evidence: form.evidence.trim(),
      });
      onSubmitted(result.data);
    } catch (error) {
      setSubmitError(error instanceof Error ? error.message : String(error));
      setSubmitting(false);
    }
  }

  function update<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((current) => ({ ...current, [key]: value }));
  }

  return <Dialog open={Boolean(decision)} onClose={() => !submitting && onClose()} maxWidth="sm" fullWidth>
    <DialogTitle><Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}><AutorenewIcon color="warning" />返炉承接 · {decision?.code}</Box></DialogTitle>
    <DialogContent dividers className="dialog-stack">
      <Typography variant="body2" color="text.secondary">
        原炉次 <b>{decision?.heatCode}</b> 将一次提交置为已拒绝，并在选定炉台生成继承原成分规格的 charged 返炉炉次，目标炉台同步进入 charging。
      </Typography>

      {optionsError && <Alert severity="error">{optionsError}</Alert>}
      {submitError && <Alert severity="error" data-testid="remelt-submit-error">{submitError}</Alert>}

      {loadingOptions ? <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, color: 'text.secondary' }}><CircularProgress size={20} />正在读取可用承接炉台…</Box>
        : <>
          {!optionsError && eligibleOptions.length === 0 && <Alert severity="warning" data-testid="remelt-no-furnace">当前没有合规炉台可承接该炉次，无法提交返炉。请先在炉台页释放满足牌号、装料量和目标温度的 available 炉台。</Alert>}
          <TextField select required label="承接炉台（当前 available 且满足牌号/装料量/温度）" value={form.furnaceCode}
            onChange={(event) => update('furnaceCode', event.target.value)} fullWidth disabled={eligibleOptions.length === 0 || submitting}>
            {eligibleOptions.map((option) => <MenuItem key={option.code} value={option.code}>
              {option.code} · {option.name} · {option.plantArea} · {option.capacityTonnes}t · ≤{option.maxTemperatureC}°C
            </MenuItem>)}
          </TextField>
          {selectedOption && <Typography variant="caption" color="text.secondary">
            承接后炉台 {selectedOption.code} 进入 charging；返炉炉次从 charged（装料）状态继续普通熔炼流程。
          </Typography>}

          {ineligibleOptions.length > 0 && <>
            <Divider sx={{ my: 0.5 }} />
            <Typography variant="subtitle2">不可承接炉台及原因</Typography>
            {ineligibleOptions.map((option) => <Alert key={option.code} severity="info" sx={{ py: 0 }}>
              <b>{option.code}</b>（{option.status}）：{option.reasons.join('；')}
            </Alert>)}
          </>}

          <Divider sx={{ my: 0.5 }} />
          <TextField required label="返炉炉次编码" value={form.returnHeatCode} inputProps={{ maxLength: 64 }}
            onChange={(event) => update('returnHeatCode', event.target.value)} fullWidth disabled={submitting} />
          <TextField required label="返炉炉次名称" value={form.returnHeatName} inputProps={{ maxLength: 160 }}
            onChange={(event) => update('returnHeatName', event.target.value)} fullWidth disabled={submitting} />
          <TextField required label="返炉原因" value={form.reason} multiline minRows={2} inputProps={{ maxLength: 500 }}
            onChange={(event) => update('reason', event.target.value)} fullWidth disabled={submitting} />
          <TextField required label="返炉证据编号" value={form.evidence} inputProps={{ maxLength: 2000 }}
            onChange={(event) => update('evidence', event.target.value)} fullWidth disabled={submitting} />
        </>}
    </DialogContent>
    <DialogActions>
      <Button onClick={onClose} disabled={submitting}>取消</Button>
      <Button variant="contained" color="warning" onClick={() => void submit()} startIcon={submitting ? <CircularProgress size={16} color="inherit" /> : <AutorenewIcon />}
        disabled={loadingOptions || submitting || !formValid}>一次提交返炉闭环</Button>
    </DialogActions>
  </Dialog>;
}
