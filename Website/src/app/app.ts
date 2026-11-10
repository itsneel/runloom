import {
  Component,
  computed,
  HostListener,
  OnDestroy,
  signal,
} from "@angular/core";
import { Icon } from "./icon";
import { definitionFor, faqs, scenarios, siteConfig } from "./content";
import { studioDestination } from "./studio-destination";

@Component({
  selector: "site-root",
  imports: [Icon],
  templateUrl: "./app.html",
  styleUrl: "./app.scss",
})
export class App implements OnDestroy {
  readonly config = {
    ...siteConfig,
    studioUrl: studioDestination(
      document.querySelector<HTMLMetaElement>('meta[name="runloom-studio-url"]')?.content,
      siteConfig.studioUrl,
    ),
  };
  readonly scenarios = scenarios;
  readonly faqs = faqs;
  readonly selected = signal(0);
  readonly scenario = computed(() => this.scenarios[this.selected()]);
  readonly json = computed(() =>
    JSON.stringify(definitionFor(this.scenario()), null, 2),
  );
  readonly jsonVisible = signal(false);
  readonly mobileMenu = signal(false);
  readonly stage = signal(-1);
  readonly inspecting = signal(2);
  readonly running = signal(false);
  readonly copyMessage = signal("");
  readonly message = computed(() =>
    this.stage() < 0
      ? "A little structure. A lot of possibility."
      : this.stage() === 2
        ? this.scenario().gate
        : this.stage() === 5
          ? "Workflow complete. Every step accounted for."
          : "Moving the work forward…",
  );
  private timer?: ReturnType<typeof setTimeout>;
  private copyTimer?: ReturnType<typeof setTimeout>;

  selectScenario(index: number): void {
    this.reset();
    this.selected.set(index);
    this.inspecting.set(2);
    this.copyMessage.set("");
  }
  reset(): void {
    clearTimeout(this.timer);
    this.stage.set(-1);
    this.running.set(false);
  }
  start(): void {
    this.reset();
    this.jsonVisible.set(false);
    this.running.set(true);
    this.stage.set(0);
    this.inspecting.set(0);
    this.advance();
  }
  private advance(): void {
    this.timer = setTimeout(() => {
      this.stage.update((value) => value + 1);
      this.inspecting.set(Math.min(this.stage(), 4));
      if (this.stage() === 2 || this.stage() === 5) this.running.set(false);
      else this.advance();
    }, 650);
  }
  continueDemo(): void {
    if (this.stage() !== 2 || this.running()) return;
    this.running.set(true);
    this.stage.set(3);
    this.inspecting.set(3);
    this.advance();
  }
  async copyJSON(): Promise<void> {
    clearTimeout(this.copyTimer);
    try {
      await navigator.clipboard.writeText(this.json());
      this.copyMessage.set("JSON copied");
    } catch {
      this.copyMessage.set("Select the JSON below to copy it manually.");
    }
    this.copyTimer = setTimeout(() => this.copyMessage.set(""), 4500);
  }
  @HostListener("document:keydown.escape") closeMenu(): void {
    this.mobileMenu.set(false);
  }
  ngOnDestroy(): void {
    clearTimeout(this.timer);
    clearTimeout(this.copyTimer);
  }
}
