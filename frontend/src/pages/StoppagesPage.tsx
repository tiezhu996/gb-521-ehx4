import { useEffect, useMemo, useState } from 'react';
import { Alert, Button, DatePicker, Form, Input, Modal, Select, Steps, Table, Tabs, Tag, message } from 'antd';
import { AlertTriangle, Ban, FilePlus2, RefreshCw, RotateCcw, Wind } from 'lucide-react';
import type { ColumnsType } from 'antd/es/table';
import dayjs, { type Dayjs } from 'dayjs';
import utc from 'dayjs/plugin/utc';
import { PageHeader } from '../components/common/PageHeader';
import { ConfirmActionDialog } from '../components/common/ConfirmActionDialog';
import { useAuth } from '../hooks/useAuth';
import { useEdgeStore } from '../stores/edgeStore';
import { useStoppageStore } from '../stores/stoppageStore';
import type { StoppageAssessment, VentilationStoppage } from '../types/stoppage';
import { ApiRequestError } from '../types/api';
import { reportError } from '../utils/errors';
import { formatDateTime, formatNumber } from '../utils/format';

dayjs.extend(utc);

const doorLabel: Record<string, string> = { open: '开启', closed: '关闭', regulating: '调节' };

function edgeLabel(code: string, edgeId: number, edges: ReturnType<typeof useEdgeStore.getState>['edges']) {
  const edge = edges.find((item) => item.id === edgeId);
  return edge ? `${edge.code} · ${edge.from_node?.code ?? edge.from_node_id} → ${edge.to_node?.code ?? edge.to_node_id}` : code;
}

function StoppageTag({ status }: { status: VentilationStoppage['status'] }) {
  return status === 'active'
    ? <Tag color="error" icon={<Ban size={12} />}>停风中</Tag>
    : <Tag color="default">已恢复</Tag>;
}

export function StoppagesPage() {
  const { hasRole } = useAuth();
  const canOperate = hasRole('engineer', 'admin');
  const canRecover = hasRole('engineer', 'reviewer', 'admin');
  const { edges, load: loadEdges } = useEdgeStore();
  const { stoppages, active, loading, load, loadActive, preview, create, recover } = useStoppageStore();
  const [registerOpen, setRegisterOpen] = useState(false);
  const [step, setStep] = useState(0);
  const [busy, setBusy] = useState(false);
  const [assessment, setAssessment] = useState<StoppageAssessment | null>(null);
  const [arrangements, setArrangements] = useState<Record<number, string>>({});
  const [recoverTarget, setRecoverTarget] = useState<VentilationStoppage | null>(null);
  const [recoverNote, setRecoverNote] = useState('');
  const [recoverBusy, setRecoverBusy] = useState(false);
  const [form] = Form.useForm<{ edge_id: number; reason: string; planned_restore_at: Dayjs }>();

  const refresh = async () => {
    try {
      await Promise.all([load(), loadActive(), loadEdges()]);
    } catch (error) {
      reportError(error, '停风登记加载失败');
    }
  };
  useEffect(() => { void refresh(); }, []);

  const activeEdgeIDs = useMemo(() => new Set(active.map((item) => item.edge_id)), [active]);
  const edgeOptions = useMemo(() => edges
    .filter((edge) => edge.enabled && edge.door_state !== 'closed')
    .map((edge) => ({
      value: edge.id,
      disabled: activeEdgeIDs.has(edge.id),
      label: `${edge.code} · ${edge.from_node?.code ?? edge.from_node_id} → ${edge.to_node?.code ?? edge.to_node_id}${activeEdgeIDs.has(edge.id) ? '（停风中）' : ''}`,
    })), [edges, activeEdgeIDs]);

  const openRegister = () => {
    setStep(0);
    setAssessment(null);
    setArrangements({});
    form.resetFields();
    form.setFieldsValue({ planned_restore_at: dayjs().add(2, 'hour') });
    setRegisterOpen(true);
  };
  const closeRegister = () => { if (!busy) { setRegisterOpen(false); } };

  const runPreview = async () => {
    const values = await form.validateFields();
    setBusy(true);
    try {
      const result = await preview(values.edge_id, values.planned_restore_at.utc().second(0).millisecond(0).toISOString());
      setAssessment(result);
      setArrangements({});
      setStep(1);
    } catch (error) {
      if (error instanceof ApiRequestError) reportError(error, '影响推演失败');
    } finally {
      setBusy(false);
    }
  };

  const submitRegistration = async () => {
    if (!assessment) return;
    const values = await form.validateFields();
    setBusy(true);
    try {
      await create({
        edge_id: values.edge_id,
        reason: values.reason.trim(),
        planned_restore_at: values.planned_restore_at.utc().second(0).millisecond(0).toISOString(),
        arrangements: assessment.workface_impacts.map((impact) => ({
          workface_node_id: impact.workface_node_id,
          arrangement: (arrangements[impact.workface_node_id] ?? '').trim(),
        })),
      });
      message.success('停风登记已提交并写入审计，推演中该巷道按风门关闭处理');
      setRegisterOpen(false);
    } catch (error) {
      reportError(error, '停风登记提交失败');
    } finally {
      setBusy(false);
    }
  };

  const doRecover = async () => {
    if (!recoverTarget) return;
    setRecoverBusy(true);
    try {
      await recover(recoverTarget.id, recoverNote.trim() || undefined);
      message.success(`停风登记 #${recoverTarget.id} 已恢复，巷道回到登记前风门状态并写入审计`);
      setRecoverTarget(null);
      setRecoverNote('');
    } catch (error) {
      reportError(error, '停风恢复失败');
    } finally {
      setRecoverBusy(false);
    }
  };

  const columns: ColumnsType<VentilationStoppage> = [
    { title: '编号', dataIndex: 'id', width: 70, render: (value) => <strong>#{value}</strong> },
    { title: '巷道', width: 210, render: (_, row) => edgeLabel(row.edge?.code ?? '', row.edge_id, edges) },
    { title: '状态', dataIndex: 'status', width: 100, render: (value) => <StoppageTag status={value} /> },
    { title: '停风原因', dataIndex: 'reason' },
    { title: '登记前风门', dataIndex: 'baseline_door_state', width: 100, render: (value) => doorLabel[value] ?? value },
    { title: '计划恢复', dataIndex: 'planned_restore_at', width: 165, render: formatDateTime },
    { title: '实际恢复', dataIndex: 'actual_restore_at', width: 165, render: formatDateTime },
    {
      title: '操作', width: 110,
      render: (_, row) => row.status === 'active'
        ? <Button size="small" type="primary" ghost icon={<RotateCcw size={14} />} disabled={!canRecover} onClick={() => setRecoverTarget(row)}>恢复通风</Button>
        : null,
    },
  ];

  const selectedEdge = assessment ? edges.find((edge) => edge.id === assessment.new_edge_id) : undefined;

  return (
    <div className="page">
      <PageHeader
        eyebrow="检修作业 / 停风登记"
        title="停风登记"
        meta={<><span>{active.length} 条在停风巷道</span><span>{stoppages.length - active.length} 条已恢复记录</span><span>登记巷道在推演中按风门关闭计算</span></>}
        actions={<><Button icon={<RefreshCw size={17} />} onClick={() => void refresh()}>刷新</Button>{canOperate && <Button type="primary" icon={<FilePlus2 size={17} />} onClick={openRegister}>补登停风</Button>}</>}
      />
      <Alert
        className="section-alert"
        type="warning"
        showIcon
        message="停风必须先登记：提交时系统会把所有在停风巷道一起纳入推演，任何工作面一点风都保不住时直接驳回并指出切断风路的巷道；仍有风但低于需风量的工作面必须写明停风安排。"
      />
      <section className="workspace-section">
        <div className="section-heading">
          <div><span className="section-index">01</span><h2>在停风巷道与登记记录</h2></div>
          <span>恢复后推演自动回到登记前风门状态，登记与恢复均写入不可变审计</span>
        </div>
        <Tabs items={[
          {
            key: 'active', label: `在停风 ${active.length}`,
            children: <Table rowKey="id" columns={columns} dataSource={active.filter((item) => item.status === 'active')} loading={loading} size="small" pagination={{ pageSize: 8 }} scroll={{ x: 1100 }} locale={{ emptyText: '当前没有在停风的巷道' }} />,
          },
          {
            key: 'all', label: `全部记录 ${stoppages.length}`,
            children: <Table rowKey="id" columns={columns} dataSource={stoppages} loading={loading} size="small" pagination={{ pageSize: 8 }} scroll={{ x: 1100 }} />,
          },
        ]} />
      </section>

      <Modal
        title="检修停风登记"
        open={registerOpen}
        width={860}
        onCancel={closeRegister}
        maskClosable={false}
        destroyOnClose
        footer={null}
      >
        <Steps
          size="small"
          current={step}
          style={{ margin: '8px 0 20px' }}
          items={[{ title: '巷道与安排' }, { title: '影响复核' }]}
        />
        {step === 0 && (
          <Form form={form} layout="vertical">
            <Form.Item
              name="edge_id"
              label="停风巷道"
              rules={[{ required: true, message: '请选择本次检修停风的巷道' }]}
            >
              <Select showSearch optionFilterProp="label" options={edgeOptions} placeholder="选择一条启用且当前未在停风的巷道" />
            </Form.Item>
            <Form.Item
              name="reason"
              label="停风原因"
              rules={[{ required: true, min: 4, message: '请填写至少 4 个字符的停风原因' }]}
            >
              <Input.TextArea rows={3} maxLength={300} showCount placeholder="例如：检修更换调节风门、处理片帮、瓦斯钻孔作业" />
            </Form.Item>
            <Form.Item
              name="planned_restore_at"
              label="计划恢复时间（UTC）"
              rules={[{ required: true, message: '请选择计划恢复时间' }]}
              extra="必须晚于当前时间；到期后仍需由有权限人员确认恢复。"
            >
              <DatePicker showTime={{ format: 'HH:mm' }} format="YYYY-MM-DD HH:mm [UTC]" style={{ width: '100%' }} />
            </Form.Item>
            <div className="modal-actions">
              <Button onClick={closeRegister} disabled={busy}>返回</Button>
              <Button type="primary" icon={<Wind size={16} />} loading={busy} onClick={() => void runPreview()}>推演停风影响</Button>
            </div>
          </Form>
        )}
        {step === 1 && assessment && (
          <div className="stoppage-assessment">
            <p className="muted">
              本次登记：{selectedEdge?.code}（{selectedEdge?.from_node?.code} → {selectedEdge?.to_node?.code}）。
              推演状态：{assessment.solver_status === 'converged' ? '已收敛' : assessment.solver_status}；
              合并在停风巷道 {assessment.combined_edge_ids.length} 条{assessment.active_edge_ids.length > 0 ? `（含既有停风 ${assessment.active_edge_ids.length} 条）` : ''}。
            </p>

            {assessment.zero_air_workfaces.length > 0 && (
              <Alert
                className="section-alert"
                type="error"
                showIcon
                icon={<Ban size={18} />}
                message="登记被驳回：存在一点风都保不住的工作面"
                description={
                  <ul className="stoppage-zero-list">
                    {assessment.zero_air_workfaces.map((zero) => (
                      <li key={zero.workface_node_id}>
                        <strong>{zero.workface_code}</strong> 无风：
                        {zero.cutting_edge_code
                          ? <>巷道 <strong>{zero.cutting_edge_code}</strong> 切断了风路（{zero.reason}）</>
                          : zero.reason}
                      </li>
                    ))}
                  </ul>
                }
              />
            )}

            {assessment.workface_impacts.length > 0 && (
              <div className="stoppage-impact-block">
                <div className="stoppage-block-title"><AlertTriangle size={16} />风量掉到需风量以下的工作面（仍有风，必须写清这次停风怎么安排）</div>
                {assessment.workface_impacts.map((impact) => (
                  <div key={impact.workface_node_id} className="stoppage-arrange-row">
                    <div className="stoppage-impact-figures">
                      <strong>{impact.workface_code}</strong>
                      <span>需风 {formatNumber(impact.required_airflow_m3s, 2)} m³/s</span>
                      <span>停风后 {formatNumber(impact.projected_airflow_m3s, 2)} m³/s</span>
                      <span className="stoppage-shortfall">缺口 {formatNumber(impact.shortfall_m3s, 2)} m³/s</span>
                    </div>
                    <Input.TextArea
                      rows={2}
                      maxLength={300}
                      showCount
                      value={arrangements[impact.workface_node_id] ?? ''}
                      onChange={(event) => setArrangements((prev) => ({ ...prev, [impact.workface_node_id]: event.target.value }))}
                      placeholder="本次停风期间该工作面的安排，例如：减产/撤人、局部通风、瓦斯员盯守、测风频次（不少于 4 个字符）"
                    />
                  </div>
                ))}
              </div>
            )}

            <div className="stoppage-impact-block">
              <div className="stoppage-block-title">相关巷道失风量（停风前 → 合并停风后）</div>
              <Table
                rowKey="edge_id"
                size="small"
                pagination={false}
                dataSource={assessment.edge_flow_losses}
                columns={[
                  { title: '巷道', dataIndex: 'edge_code', width: 110, render: (value) => <strong>{value}</strong> },
                  { title: '停风前 m³/s', dataIndex: 'baseline_flow_m3s', width: 110, render: (value) => formatNumber(Math.abs(value), 2) },
                  { title: '停风后 m³/s', dataIndex: 'projected_flow_m3s', width: 110, render: (value) => formatNumber(Math.abs(value), 2) },
                  {
                    title: '失风量 m³/s', dataIndex: 'flow_loss_m3s', width: 110,
                    render: (value) => <span className="stoppage-shortfall">{formatNumber(value, 2)}</span>,
                  },
                  {
                    title: '说明', width: 170,
                    render: (_, row) => row.closed
                      ? (row.already_stopped ? <Tag color="orange">既有停风（已关）</Tag> : <Tag color="error">本次停风（按关闭）</Tag>)
                      : <Tag>风路受影响</Tag>,
                  },
                ]}
              />
            </div>

            <div className="modal-actions">
              <Button onClick={() => setStep(0)} disabled={busy}>返回修改</Button>
              <Button
                type="primary"
                danger
                icon={<Ban size={16} />}
                loading={busy}
                disabled={assessment.zero_air_workfaces.length > 0}
                onClick={() => void submitRegistration()}
              >
                {assessment.zero_air_workfaces.length > 0 ? '存在无风工作面，无法登记' : '确认提交登记'}
              </Button>
            </div>
          </div>
        )}
      </Modal>

      <ConfirmActionDialog
        open={recoverTarget !== null}
        title={`恢复停风：#${recoverTarget?.id ?? ''} ${recoverTarget?.edge?.code ?? ''}`}
        consequence="恢复后该巷道立即回到登记前的风门状态参与推演，实际恢复时间与操作人会写入不可变审计；请确认现场测风、瓦斯检查已完成且风路畅通。"
        confirmLabel="确认恢复通风"
        noteLabel="恢复说明（现场确认情况）"
        note={recoverNote}
        busy={recoverBusy}
        onNoteChange={setRecoverNote}
        onCancel={() => { setRecoverTarget(null); setRecoverNote(''); }}
        onConfirm={() => void doRecover()}
      />
    </div>
  );
}
