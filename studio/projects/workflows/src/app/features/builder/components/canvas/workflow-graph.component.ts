import { CommonModule } from '@angular/common';
import {
  AfterViewInit,
  ChangeDetectionStrategy,
  ChangeDetectorRef,
  Component,
  DoCheck,
  ElementRef,
  EventEmitter,
  HostListener,
  Input,
  OnDestroy,
  Output,
  ViewChild,
  inject,
} from '@angular/core';
import { UiIcon } from '@runloom/studio-platform';
import type { WorkflowStep, WorkflowSignalHandler } from '../../../../core/models/workflow.types';
import type { StepLocation } from '../../model/workflow-tree';
import { buildCanvas, edgeGeometry, NODE_HEIGHT, NODE_WIDTH, zoomAt } from './canvas-layout';
import type { CanvasEdge, CanvasNode, CanvasScene, Point } from './canvas-layout';

@Component({
  selector: 'runloom-workflow-graph',
  changeDetection: ChangeDetectionStrategy.Default,
  imports: [CommonModule, UiIcon],
  templateUrl: './workflow-graph.component.html',
  styleUrl: './workflow-graph.component.scss',
})
export class WorkflowGraph implements DoCheck, AfterViewInit, OnDestroy {
  @Input() handlers: WorkflowSignalHandler[] = [];
  @Output() handlerSelected = new EventEmitter<string>();
  @Input() steps: WorkflowStep[] = [];
  @Input() nodeErrors: Record<string, string> = {};
  @Input() positions: Record<string, Point> = {};
  @Input() workflowKey = '';
  @Input() selected?: WorkflowStep;
  @Input() editable = false;
  @Input() palette: Array<{ type: string; label: string; icon: string }> = [];
  @Output() selectedNode = new EventEmitter<StepLocation>();
  @Output() insertNode = new EventEmitter<StepLocation>();
  @Output() duplicateNode = new EventEmitter<StepLocation>();
  @Output() deleteNode = new EventEmitter<StepLocation>();
  @Output() moveNode = new EventEmitter<{ from: StepLocation; to: StepLocation }>();
  @Output() addBranch = new EventEmitter<WorkflowStep>();
  @Output() layoutChanged = new EventEmitter<Record<string, Point>>();
  @ViewChild('viewport') viewport?: ElementRef<HTMLElement>;
  private readonly cdr = inject(ChangeDetectorRef);
  scene: CanvasScene = { nodes: [], edges: [] };
  private signature = '';
  private sourceSteps?: WorkflowStep[];
  private documentKey = '';
  private topology = '';
  private frame = 0;
  zoom = 1;
  pan: Point = { x: 60, y: 180 };
  space = false;
  moving?: StepLocation;
  gesture?: { pointer: number; start: Point; origin: Point; node?: CanvasNode; moved: boolean };
  private suppressClick = false;

  ngDoCheck(): void {
    // Include object replacement, so connection targets never keep stale arrays
    // after save/reload. Pointer movement changes only scene coordinates.
    const signature = JSON.stringify([this.workflowKey, this.steps, this.positions, this.handlers]);
    if (signature !== this.signature || this.sourceSteps !== this.steps) {
      const count = this.scene.nodes.length;
      this.sourceSteps = this.steps;
      this.signature = signature;
      this.scene = buildCanvas(this.steps, this.positions, this.handlers);
      const topology = JSON.stringify([
        this.scene.nodes.map((node) => node.key),
        this.scene.edges.map((edge) => [edge.from, edge.to]),
      ]);
      if (count !== this.scene.nodes.length || topology !== this.topology) this.scheduleFit();
      this.topology = topology;
      this.moving = undefined;
      if (this.documentKey !== this.workflowKey) {
        this.documentKey = this.workflowKey;
        this.cancelGesture();
        this.scheduleFit();
      }
    }
    if (!this.editable) {
      this.moving = undefined;
      if (this.gesture?.node) this.cancelGesture();
    }
  }
  ngAfterViewInit(): void {
    this.scheduleFit();
  }
  ngOnDestroy(): void {
    cancelAnimationFrame(this.frame);
  }
  private scheduleFit(): void {
    cancelAnimationFrame(this.frame);
    this.frame = requestAnimationFrame(() => {
      this.center();
      this.cdr.markForCheck();
    });
  }
  labelForNode(step: WorkflowStep): string {
    return this.palette.find((item) => item.type === step.type)?.label || step.type;
  }
  icon(step: WorkflowStep): string {
    return this.palette.find((item) => item.type === step.type)?.icon || '◇';
  }
  trackNode(_index: number, node: CanvasNode): string {
    return node.key;
  }
  trackEdge(_index: number, edge: CanvasEdge): string {
    return edge.key;
  }
  geometry(edge: CanvasEdge): ReturnType<typeof edgeGeometry> {
    return edgeGeometry(
      this.scene.nodes.find((node) => node.key === edge.from)!,
      this.scene.nodes.find((node) => node.key === edge.to)!,
    );
  }
  get transform(): string {
    return `translate(${this.pan.x}px, ${this.pan.y}px) scale(${this.zoom})`;
  }
  onWheel(event: WheelEvent): void {
    event.preventDefault();
    if (this.gesture) return;
    const factor =
      event.deltaMode === 1
        ? 16
        : event.deltaMode === 2
          ? this.viewport!.nativeElement.clientHeight
          : 1;
    if (event.ctrlKey || event.metaKey) {
      const rect = this.viewport!.nativeElement.getBoundingClientRect();
      this.setZoom(this.zoom * Math.exp(-event.deltaY * factor * 0.008), {
        x: event.clientX - rect.left,
        y: event.clientY - rect.top,
      });
    } else
      this.pan = { x: this.pan.x - event.deltaX * factor, y: this.pan.y - event.deltaY * factor };
  }
  private setZoom(value: number, anchor?: Point): void {
    const element = this.viewport?.nativeElement;
    if (!element) return;
    const next = Math.max(0.15, Math.min(2.5, value));
    this.pan = zoomAt(
      this.pan,
      this.zoom,
      next,
      anchor ?? { x: element.clientWidth / 2, y: element.clientHeight / 2 },
    );
    this.zoom = next;
  }
  changeZoom(delta: number): void {
    if (!this.gesture) this.setZoom(Math.round((this.zoom + delta) * 100) / 100);
  }
  resetZoom(): void {
    this.setZoom(1);
  }
  center(): void {
    const viewport = this.viewport?.nativeElement;
    if (!viewport || !this.scene.nodes.length || this.gesture) return;
    const minX = Math.min(...this.scene.nodes.map((n) => n.x)),
      minY = Math.min(...this.scene.nodes.map((n) => n.y));
    const maxX = Math.max(...this.scene.nodes.map((n) => n.x + NODE_WIDTH)),
      maxY = Math.max(...this.scene.nodes.map((n) => n.y + NODE_HEIGHT));
    this.zoom = Math.max(
      0.15,
      Math.min(
        1,
        (viewport.clientWidth - 96) / (maxX - minX),
        (viewport.clientHeight - 96) / (maxY - minY),
      ),
    );
    this.pan = {
      x: (viewport.clientWidth - (maxX - minX) * this.zoom) / 2 - minX * this.zoom,
      y: (viewport.clientHeight - (maxY - minY) * this.zoom) / 2 - minY * this.zoom,
    };
  }
  autoLayout(): void {
    if (!this.editable) return;
    this.cancelGesture();
    this.positions = {};
    this.scene = buildCanvas(this.steps, {}, this.handlers);
    this.positions = Object.fromEntries(
      this.scene.nodes.map((node) => [node.key, { x: node.x, y: node.y }]),
    );
    this.layoutChanged.emit(this.positions);
    this.scheduleFit();
  }
  startPointer(event: PointerEvent, node?: CanvasNode): void {
    if ((event.button !== 0 && event.button !== 1) || this.gesture) return;
    if (
      !node &&
      !this.space &&
      event.button !== 1 &&
      (event.target as HTMLElement).closest('button, article')
    )
      return;
    if (
      node &&
      !this.space &&
      event.button !== 1 &&
      (!this.editable || (event.target as HTMLElement).closest('.node-actions'))
    )
      return;
    event.stopPropagation();
    if (this.space || event.button === 1 || !node) event.preventDefault();
    if (!node) this.viewport?.nativeElement.focus({ preventScroll: true });
    const dragged =
      this.space || event.button === 1 || !node
        ? undefined
        : this.scene.nodes.find((current) => current.key === node.key);
    this.gesture = {
      pointer: event.pointerId,
      start: { x: event.clientX, y: event.clientY },
      origin: dragged ? { x: dragged.x, y: dragged.y } : { ...this.pan },
      node: dragged,
      moved: false,
    };
    this.suppressClick = false;
  }
  movePointer(event: PointerEvent): void {
    const gesture = this.gesture;
    if (!gesture || gesture.pointer !== event.pointerId) return;
    const dx = event.clientX - gesture.start.x,
      dy = event.clientY - gesture.start.y;
    if (!gesture.moved && Math.hypot(dx, dy) < 4) return;
    gesture.moved = true;
    this.suppressClick = true;
    this.viewport?.nativeElement.setPointerCapture(event.pointerId);
    event.preventDefault();
    if (gesture.node) {
      gesture.node.x = Math.max(-100000, Math.min(100000, gesture.origin.x + dx / this.zoom));
      gesture.node.y = Math.max(-100000, Math.min(100000, gesture.origin.y + dy / this.zoom));
    } else this.pan = { x: gesture.origin.x + dx, y: gesture.origin.y + dy };
    this.cdr.markForCheck();
  }
  @HostListener('document:pointerup', ['$event'])
  finishPointer(event: PointerEvent): void {
    const gesture = this.gesture;
    if (!gesture || gesture.pointer !== event.pointerId) return;
    this.gesture = undefined;
    if (this.viewport?.nativeElement.hasPointerCapture(event.pointerId))
      this.viewport.nativeElement.releasePointerCapture(event.pointerId);
    if (gesture.moved && gesture.node && this.editable)
      this.layoutChanged.emit(
        Object.fromEntries(this.scene.nodes.map((node) => [node.key, { x: node.x, y: node.y }])),
      );
  }
  cancelGesture(): void {
    const gesture = this.gesture;
    if (gesture?.node) Object.assign(gesture.node, gesture.origin);
    this.gesture = undefined;
    if (gesture && this.viewport?.nativeElement.hasPointerCapture(gesture.pointer))
      this.viewport.nativeElement.releasePointerCapture(gesture.pointer);
  }
  select(event: MouseEvent, node: CanvasNode): void {
    if (this.suppressClick) {
      this.suppressClick = false;
      event.preventDefault();
      event.stopPropagation();
      return;
    }
    if (node.handler) this.handlerSelected.emit(node.handler.id);
    else if (node.location) this.selectedNode.emit(node.location);
  }
  insert(edge: CanvasEdge): void {
    if (!this.editable || !edge.insert) return;
    if (this.moving) {
      this.moveNode.emit({ from: this.moving, to: edge.insert });
      this.moving = undefined;
    } else this.insertNode.emit(edge.insert);
  }
  nudge(event: KeyboardEvent, node: CanvasNode): void {
    if (
      !this.editable ||
      !event.altKey ||
      !['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(event.key)
    )
      return;
    node = this.scene.nodes.find((current) => current.key === node.key) ?? node;
    event.preventDefault();
    event.stopPropagation();
    const delta = event.shiftKey ? 40 : 10;
    node.x = Math.max(
      -100000,
      Math.min(
        100000,
        node.x + (event.key === 'ArrowRight' ? delta : event.key === 'ArrowLeft' ? -delta : 0),
      ),
    );
    node.y = Math.max(
      -100000,
      Math.min(
        100000,
        node.y + (event.key === 'ArrowDown' ? delta : event.key === 'ArrowUp' ? -delta : 0),
      ),
    );
    this.layoutChanged.emit(
      Object.fromEntries(this.scene.nodes.map((n) => [n.key, { x: n.x, y: n.y }])),
    );
  }
  @HostListener('document:keydown', ['$event']) keyDown(event: KeyboardEvent): void {
    if ((event.target as HTMLElement).closest('input, textarea, select, [contenteditable="true"]'))
      return;
    if (event.code === 'Space' && this.viewport?.nativeElement.contains(document.activeElement)) {
      event.preventDefault();
      this.space = true;
    }
    if (event.key === 'Escape') {
      this.cancelGesture();
      this.moving = undefined;
    }
  }
  @HostListener('document:keyup', ['$event']) keyUp(event: KeyboardEvent): void {
    if (event.code === 'Space') this.space = false;
  }
  @HostListener('window:blur') blur(): void {
    this.space = false;
    this.cancelGesture();
  }
}
