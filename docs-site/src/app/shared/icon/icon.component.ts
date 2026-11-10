import { Component, input } from '@angular/core';

@Component({
  selector: 'docs-icon',
  templateUrl: './icon.component.html',
  styleUrl: './icon.component.scss',
})
export class IconComponent {
  readonly name = input('arrow');
  readonly paths: Record<string, string> = {
    arrow: 'M5 12h14m-6-6 6 6-6 6',
    search: 'M21 21l-5-5M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0',
    book: 'M4 3h12a3 3 0 0 1 3 3v15H7a3 3 0 0 1-3-3V3Zm0 15a3 3 0 0 1 3-3h12M8 7h7M8 10h5',
    code: 'm8 7-5 5 5 5m8-10 5 5-5 5m-3-13-2 20',
    link: 'm10 13 4-4m-6 6-2 2a4 4 0 0 1-6-6l5-5a4 4 0 0 1 6 0m2 3 2-2a4 4 0 0 1 6 6l-5 5a4 4 0 0 1-6 0',
    layers: 'm12 3 10 5-10 5L2 8l10-5ZM2 12l10 5 10-5M2 16l10 5 10-5',
    sun: 'M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1 1m12 12 1 1M5 19l1-1M18 6l1-1M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0',
    moon: 'M20 15A9 9 0 0 1 9 4a9 9 0 1 0 11 11Z',
    menu: 'M4 6h16M4 12h16M4 18h16',
    close: 'm6 6 12 12M6 18 18 6',
    external: 'M14 3h7v7m0-7L10 14M10 3H4v17h17v-7',
    bolt: 'm13 2-9 12h7l-1 8 10-13h-7l1-7Z',
    mail: 'M3 5h18v14H3V5Zm0 0 9 8 9-8',
    sliders: 'M4 7h16M4 17h16M8 4v6m8 4v6',
    check: 'm5 12 4 4L19 6',
  };
}
