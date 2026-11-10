import { Component, Input } from '@angular/core';

@Component({
  selector: 'runloom-icon',
  templateUrl: './ui-icon.html',
  styleUrl: './ui-icon.scss',
})
export class UiIcon {
  @Input() name = 'workflow';
  readonly paths: Record<string, string> = {
    'chevron-down': 'm6 9 6 6 6-6',
    'chevron-up': 'm6 15 6-6 6 6',
    minus: 'M5 12h14',
    check: 'm5 12 4 4L19 6',
    play: 'm8 5 11 7-11 7V5Z',
    copy: 'M9 9h12v12H9V9ZM15 9V3H3v12h6',
    target: 'M12 7a5 5 0 1 0 0 10 5 5 0 0 0 0-10ZM12 2v5m0 10v5M2 12h5m10 0h5',
    sun: 'M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8ZM12 2v2m0 16v2M2 12h2m16 0h2M5 5l1 1m12 12 1 1M5 19l1-1M18 6l1-1',
    moon: 'M20 15A9 9 0 0 1 9 4a9 9 0 1 0 11 11Z',
    monitor: 'M3 4h18v13H3V4Zm9 13v4m-4 0h8',
    budget: 'M3 5h18v15H3V5Zm0 4h18m-6 4h6v4h-6v-4ZM6 2v3m12-3v3',
    analytics: 'M4 3v18h17M8 16v-4m5 4V8m5 8V5',
    sidebar: 'M4 3h16a1 1 0 0 1 1 1v16a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1Zm4 0v18',
    workflow: 'M3 3h6v6H3Zm12 12h6v6h-6ZM9 6h6a3 3 0 0 1 3 3v6M6 9v6a3 3 0 0 0 3 3h6',
    plus: 'M12 5v14M5 12h14',
    runs: 'm9 6 10 6-10 6V6ZM4 5v14',
    calendar:
      'M5 5h14a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2Zm2-3v6m10-6v6M3 11h18m-14 4h2m4 0h2m-8 3h2',
    team: 'M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2m20 0v-2a4 4 0 0 0-3-3.87M9 3a4 4 0 1 0 0 8 4 4 0 0 0 0-8Zm8 0a4 4 0 0 1 0 8',
    building: 'M4 21V5l8-3v19m0-14h8v14M2 21h20M7 7h1m-1 4h1m-1 4h1m8-4h1m-1 4h1',
    key: 'M14 4a6 6 0 1 0 6 6 6 6 0 0 0-6-6ZM10 14l-7 7m0-4v4h4m10-13h.01',
    shield: 'm12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6l8-3Zm-3 9 2 2 4-4',
    logout: 'M9 4H4v16h5m6-13 5 5-5 5M8 12h12',
    arrow: 'M5 12h14m-6-6 6 6-6 6',
    search: 'M10 3a7 7 0 1 0 0 14 7 7 0 0 0 0-14Zm5 12 6 6',
    folder: 'M3 7V5h6l2 3h10v12H3V7Z',
    close: 'm6 6 12 12M18 6 6 18',
    edit: 'm15 4 5 5M4 20l5-1L21 7l-5-5L4 14v6Z',
  };
}
