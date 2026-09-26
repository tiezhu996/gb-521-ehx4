import { useEffect, useMemo, useState } from 'react';
import { Alert, Button, DatePicker, Form, Input, Select, Table, message } from 'antd';
import { ClipboardList, Play, RotateCcw, Wind } from 'lucide-react';
import type { ColumnsType } from 'antd/es/table';
import dayjs, { type Dayjs } from 'dayjs';
import { PageHeader } from '../components/common/PageHeader';
import { StatusBadge } from '../components/common/StatusBadge';
import { useAuth } from '../hooks/useAuth';
import { useEdgeStore } from '../stores/edgeStore';
import { useStoppageStore } from '../stores/stoppageStore';
import { previewStoppage } from '../api/stoppages';
import type { AirStoppage, StoppageEvaluation, WorkfaceAirDeficit } from '../types/stoppage';
import { reportError } from '../utils/errors';
import { formatDateTime, formatNumber } from '../utils/format';

interface StoppageFormValues {
  edge_id: number;
  reason: string;
  planned_restore_at: Dayjs;
}

export function StoppagesPage() {
  const { hasRole } = useAuth();
  const { edges, load: loadEdges } = useEdgeStore();
  const { stoppages, loading, load, create, restore } = useStoppageStore();
  const [form] = Form.useForm<StoppageFormValues>();
  const [evaluation, setEvaluation] = useState<StoppageEvaluation | null>(null);
  const [evaluating, setEvaluating] = useState(false);
  const [arrangements, setArrangements] = useState<Record<number, string>>({});
  const [submitting, setSubmitting] = useState(false);
  const [restoringId, setRestoringId] = useState<number | null>(null);
  const canOperate = hasRole('engineer', 'admin');

  const refresh = async () => {
    try { await Promise.all([load(), loadEdges()]); } catch (error) { reportError(error, '停风数据加载失败'); }
  };
  useEffect(() => { void refresh(); }, []);

  const activeStoppages = useMemo(() => stoppages.filter((item) => item.status === 'stopping'), [stoppages]);
  const stoppedEdgeIds = useMemo(() => new Set(activeStoppages.map((item) => item.edge_id)), [activeStoppages]);
  const edgeOptions = useMemo(() => edges
    .filter((edge) => edge.enabled)
    .map((edge) => ({
      value: edge.id,
      label: `${edge.code} · ${edge.from_node?.code ?? edge.from_node_id} → ${edge.to_node?.code ?? edge.to_node_id}${stoppedEdgeIds.has(edge.id) ? '（已在停风）' : ''}`,
      disabled: stoppedEdgeIds.has(edge.id),
    })), [edges, stoppedEdgeIds]);

  const selectedEdgeId = Form.useWatch('edge_id', form);
  useEffect(() => { setEvaluation(null); setArrangements({}); }, [selectedEdgeId]);

  const runEvaluation = async () => {
    const edgeId = form.getFieldValue('edge_id') as number | undefined;
    if (!edgeId) { void message.warning('请先选择要停风的巷道'); return; }
    setEvaluating(true);
    try {
      const result = await previewStoppage(edgeId);
      setEvaluation(result);
      setArrangements({});
      if (result.zero_air_workfaces.length > 0) void message.error('评估发现工作面将完全失风，本次登记会被驳回');
    } catch (error) { reportError(error, '停风影响评估失败'); setEvaluation(null); } finally { setEvaluating(false); }
  };

  const missingArrangements = (evaluation?.deficits ?? []).filter((deficit) => (arrangements[deficit.node_id] ?? '').trim().length < 4);
  const blocked = Boolean(evaluation && evaluation.zero_air_workfaces.length > 0);

  const submit = async (values: StoppageFormValues) => {
    if (!evaluation || blocked) return;
    setSubmitting(true);
    try {
      await create({
        edge_id: values.edge_id,
        reason: values.reason.trim(),
        planned_restore_at: values.planned_restore_at.toISOString(),
        arrangements: (evaluation.deficits ?? []).map((deficit) => ({ node_id: deficit.node_id, note: (arrangements[deficit.node_id] ?? '').trim() })),
      });
      message.success('停风登记已提交，巷道在推演中按风门关闭计算');
      form.resetFields();
      setEvaluation(null);
      setArrangements({});
    } catch (error) { reportError(error, '停风登记提交失败'); } finally { setSubmitting(false); }
  };

  const doRestore = async (item: AirStoppage) => {
    setRestoringId(item.id);
    try { await restore(item.id); message.success(`巷道 ${item.edge?.code ?? item.edge_id} 已恢复，推演回到原风门状态`); } catch (error) { reportError(error, '恢复停风失败'); } finally { setRestoringId(null); }
  };

  const deficitColumns: ColumnsType<WorkfaceAirDeficit> = [
    { title: '工作面', dataIndex: 'code', width: 110, render: (value) => <strong>{value}</strong> },
    { title: '需风量', dataIndex: 'required_m3s', width: 110, render: (value) => `${formatNumber(value)} m³/s` },
    { title: '停风后风量', dataIndex: 'stopped_m3s', width: 130, render: (value) => `${formatNumber(value)} m³/s` },
    { title: '缺口', dataIndex: 'deficit_m3s', width: 110, render: (value) => <strong className="danger-text">-{formatNumber(value)} m³/s</strong> },
    {
      title: '本次停风安排（必填）',
      dataIndex: 'node_id',
      render: (nodeId: number) => (
        <Input.TextArea
          rows={2}
          maxLength={300}
          placeholder="说明该工作面在停风期间的人员、测风与恢复安排"
          value={arrangements[nodeId] ?? ''}
          onChange={(event) => setArrangements((prev) => ({ ...prev, [nodeId]: event.target.value }))}
        />
      ),
    },
  ];

  const historyColumns: ColumnsType<AirStoppage> = [
    { title: '巷道', width: 150, render: (_, row) => <strong>{row.edge?.code ?? `#${row.edge_id}`}</strong> },
    { title: '方向', width: 170, render: (_, row) => row.edge ? `${row.edge.from_node?.code ?? row.edge.from_node_id} → ${row.edge.to_node?.code ?? row.edge.to_node_id}` : '—' },
    { title: '原因', dataIndex: 'reason', width: 240 },
    { title: '状态', dataIndex: 'status', width: 105, render: (value) => <StatusBadge status={value} /> },
    { title: '登记时间', dataIndex: 'created_at', width: 165, render: formatDateTime },
    { title: '计划恢复', dataIndex: 'planned_restore_at', width: 165, render: formatDateTime },
    { title: '实际恢复', dataIndex: 'restored_at', width: 165, render: formatDateTime },
    {
      title: '操作', width: 110, render: (_, row) => row.status === 'stopping' && canOperate
        ? <Button size="small" icon={<RotateCcw size={14} />} loading={restoringId === row.id} onClick={() => void doRestore(row)}>恢复</Button>
        : <span className="muted">—</span>,
    },
  ];

  return (
    <div className="page">
      <PageHeader
        eyebrow="停风登记 / 风门联算"
        title="检修停风登记"
        meta={<><span>{activeStoppages.length} 条巷道停风中</span><span>{stoppages.length} 条历史登记</span><span>登记与恢复均写入审计</span></>}
      />
      <Alert className="section-alert" type="warning" showIcon message="停风登记是离线评估与留痕，不替代现场停风审批；登记中的巷道在推演里按风门关闭计算，恢复后回到原状态" />
      <section className="workspace-section" aria-labelledby="stoppage-form-heading">
        <div className="section-heading"><div><span className="section-index">01</span><h2 id="stoppage-form-heading">新停风登记</h2></div><span>评估会把已在停风的巷道一起算进去</span></div>
        {canOperate ? (
          <Form form={form} layout="vertical" onFinish={submit} className="stoppage-form">
            <div className="form-grid">
              <Form.Item name="edge_id" label="停风巷道" rules={[{ required: true, message: '请选择巷道' }]}>
                <Select showSearch optionFilterProp="label" options={edgeOptions} placeholder="选择要停风的巷道" />
              </Form.Item>
              <Form.Item name="planned_restore_at" label="计划恢复时间" rules={[{ required: true, message: '请选择恢复时间' }]}>
                <DatePicker showTime style={{ width: '100%' }} disabledDate={(current) => current.isBefore(dayjs().startOf('day'))} placeholder="选择预计恢复时间" />
              </Form.Item>
            </div>
            <Form.Item name="reason" label="停风原因" rules={[{ required: true, min: 4, message: '请填写至少 4 个字符的原因' }]}>
              <Input.TextArea rows={2} maxLength={400} showCount placeholder="例如：检修 AW-102 风门执行器，需关闭该巷风路" />
            </Form.Item>
            <div className="stoppage-actions">
              <Button icon={<Play size={16} />} loading={evaluating} disabled={!selectedEdgeId} onClick={() => void runEvaluation()}>评估停风影响</Button>
              <Button type="primary" htmlType="submit" icon={<ClipboardList size={16} />} loading={submitting} disabled={!evaluation || blocked || missingArrangements.length > 0}>提交停风登记</Button>
            </div>
          </Form>
        ) : <p className="muted">当前角色只能查看停风状态，登记与恢复需要工程师或管理员角色。</p>}
        {evaluation && (
          <div className="evaluation-result">
            {blocked ? (
              <Alert
                type="error"
                showIcon
                message="登记被驳回：有工作面一点风都保不住"
                description={evaluation.zero_air_workfaces.map((zero) => (
                  <div key={zero.node_id}>
                    工作面 <strong>{zero.code}</strong> 将完全失风，切断风路的巷道：
                    {zero.cut_edge_codes.length > 0 ? zero.cut_edge_codes.map((code) => <code key={code} className="cut-edge-code">{code}</code>) : '既有关闭风门或网络问题'}
                  </div>
                ))}
              />
            ) : (
              <Alert type="success" showIcon message={`评估通过：本次与已在停风的 ${evaluation.included_edge_ids.length} 条巷道联算后，所有工作面仍保有风量`} />
            )}
            {evaluation.deficits.length > 0 && (
              <>
                <h3 className="evaluation-subheading">风量低于需风量的工作面（须写清本次停风安排）</h3>
                <Table rowKey="node_id" columns={deficitColumns} dataSource={evaluation.deficits} size="small" pagination={false} />
                {missingArrangements.length > 0 && <Alert className="section-alert" type="warning" showIcon message={`还有 ${missingArrangements.length} 个工作面未填写停风安排，填好后才能提交`} />}
              </>
            )}
            {evaluation.edge_losses.length > 0 && (
              <>
                <h3 className="evaluation-subheading">相关巷道失风量</h3>
                <Table
                  rowKey="edge_id"
                  size="small"
                  pagination={false}
                  dataSource={evaluation.edge_losses}
                  columns={[
                    { title: '巷道', dataIndex: 'code', width: 120, render: (value) => <strong>{value}</strong> },
                    { title: '正常风量', dataIndex: 'normal_m3s', width: 140, render: (value) => `${formatNumber(value)} m³/s` },
                    { title: '停风后风量', dataIndex: 'stopped_m3s', width: 140, render: (value) => `${formatNumber(value)} m³/s` },
                    { title: '失风量', dataIndex: 'loss_m3s', render: (value) => <strong className="danger-text">-{formatNumber(value)} m³/s</strong> },
                  ]}
                />
              </>
            )}
            {evaluation.deficits.length === 0 && evaluation.edge_losses.length === 0 && !blocked && (
              <p className="muted">本次停风不造成工作面需风缺口，也没有可量化的巷道失风。</p>
            )}
          </div>
        )}
      </section>
      <section className="workspace-section" aria-labelledby="stoppage-active-heading">
        <div className="section-heading"><div><span className="section-index">02</span><h2 id="stoppage-active-heading">停风登记记录</h2></div><span>恢复后巷道在推演中回到原风门状态</span></div>
        <Table rowKey="id" columns={historyColumns} dataSource={stoppages} loading={loading} size="small" pagination={{ pageSize: 8 }} scroll={{ x: 1150 }} />
      </section>
      {activeStoppages.length === 0 && (
        <div className="empty-state"><Wind size={28} /><h2>当前没有停风中的巷道</h2><p>登记检修停风后，这里和网络页会同步显示。</p></div>
      )}
    </div>
  );
}
