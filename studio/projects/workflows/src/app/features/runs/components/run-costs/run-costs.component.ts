import { CommonModule } from '@angular/common';
import { ChangeDetectorRef, Component, Input, OnChanges, OnDestroy, inject } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { WorkflowApiService } from '../../../../core/api/workflow-api.service';
import { LLMUsageEntry, LLMUsagePage } from '../../../../core/models/workflow.types';

@Component({
  selector: 'runloom-run-costs', imports: [CommonModule, FormsModule],
  styleUrl: './run-costs.component.scss',
  templateUrl: './run-costs.component.html',
})
export class RunCosts implements OnChanges, OnDestroy {
  @Input({required:true}) workflowId = '';
  @Input() running = false;
  private readonly api = inject(WorkflowApiService);
  private readonly detector = inject(ChangeDetectorRef);
  private generation = 0;
  private timer?: ReturnType<typeof setInterval>;
  private lastId = '';
  private appliedModel = ''; private appliedStep = '';
  data?: LLMUsagePage; error = ''; model = ''; step = ''; busy = false;
  ngOnChanges(): void {
    this.generation++; this.busy=false;
    if (this.timer) clearInterval(this.timer);
    if (this.workflowId !== this.lastId) { this.lastId=this.workflowId; this.model=''; this.step=''; this.data=undefined; this.generation++; this.busy=false; }
    void this.reload();
    if (this.running) this.timer=setInterval(()=>{if (!this.busy) void this.reload();},5000);
  }
  ngOnDestroy(): void { this.generation++; if(this.timer) clearInterval(this.timer); }
  money(value: string): string { const n=Number(value); return n>0 && n<0.000001 ? '<$0.000001' : '$'+n.toLocaleString('en-US',{minimumFractionDigits:2,maximumFractionDigits:6}); }
  track(_:number,entry:LLMUsageEntry):string{return entry.id;}
  async reload(): Promise<void> { if(this.busy)return; this.appliedModel=this.model;this.appliedStep=this.step;await this.load(''); }
  async more(): Promise<void> { if(!this.busy && this.data?.next_cursor)await this.load(this.data.next_cursor); }
  private async load(after:string):Promise<void>{
    const generation=this.generation,id=this.workflowId;if(!id)return;
    this.busy=true;this.error='';
    try{
      const value=await this.api.llmUsage(id,after,this.appliedModel,this.appliedStep);
      if(generation!==this.generation)return;
      if(!value?.summary || !Array.isArray(value.entries))throw new Error('Unavailable');
      this.data={...value,entries:after?[...(this.data?.entries || []),...value.entries]:value.entries};
    }catch{if(generation===this.generation){this.error='Cost accounting is unavailable. Refresh to retry; missing data is not zero cost.';this.data=undefined;}}
    finally{if(generation===this.generation){this.busy=false;this.detector.markForCheck();}}
  }
}
