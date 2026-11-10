import { Component, input } from "@angular/core";

@Component({
  selector: "site-icon",
  templateUrl: './icon.html',
  styleUrl: './icon.scss',
})
export class Icon {
  readonly name = input("flow");
  readonly paths: Record<string, string> = {
    flow: "M4 5h7v14h9M11 12h9M16 8l4 4-4 4",
    arrow: "M4 12h16M14 6l6 6-6 6",
    external: "M7 17 17 7M7 7h10v10",
    bolt: "m13 2-9 12h7l-1 8 10-13h-7l1-7",
    text: "M5 5h14M12 5v14M8 19h8",
    person: "M16 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0M4 21v-2a8 8 0 0 1 16 0v2",
    check: "m5 12 4 4L19 6",
    flag: "M5 22V3m0 0c5-4 9 4 14 0v11c-5 4-9-4-14 0",
    return: "M7 7h8a5 5 0 0 1 0 10h-3M7 7l4-4M7 7l4 4",
    spark: "m12 3 2.5 6.5L21 12l-6.5 2.5L12 21l-2.5-6.5L3 12l6.5-2.5L12 3",
    clock: "M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0M12 6v6l4 3",
    layers: "m12 2 10 5-10 5L2 7l10-5M2 12l10 5 10-5M2 17l10 5 10-5",
    code: "m8 6-6 6 6 6m8-12 6 6-6 6m-3-15-2 18",
    shield: "m12 2 8 4v6c0 5-8 10-8 10S4 17 4 12V6l8-4m-4 10 3 3 5-6",
    play: "m8 4 12 8-12 8V4",
    copy: "M9 9h12v12H9V9M5 15H3V3h12v2",
    menu: "M4 6h16M4 12h16M4 18h16",
    close: "m6 6 12 12M18 6 6 18",
  };
}
