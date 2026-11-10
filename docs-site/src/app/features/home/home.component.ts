import { Component, inject } from '@angular/core';
import { RouterLink } from '@angular/router';
import { Meta } from '@angular/platform-browser';
import { IconComponent } from '../../shared/icon/icon.component';

@Component({
  selector: 'docs-home',
  imports: [RouterLink, IconComponent],
  templateUrl: './home.component.html',
  styleUrl: './home.component.scss',
})
export class HomeComponent {
  readonly cards = [
    {
      icon: 'book',
      title: 'Build your first workflow',
      text: 'From a blank canvas to a completed run.',
      route: '/guides/first-workflow',
    },
    {
      icon: 'link',
      title: 'Connect your tools',
      text: 'Bring your databases and services together.',
      route: '/integrations/connections',
    },
    {
      icon: 'code',
      title: 'Explore the API',
      text: 'Integrate with clear, scoped interfaces.',
      route: '/api/overview',
    },
    {
      icon: 'layers',
      title: 'Deploy Runloom',
      text: 'Run the platform in your own environment.',
      route: '/deployment/helm',
    },
  ];
  constructor() {
    inject(Meta).updateTag({
      name: 'description',
      content: 'Learn to build, run and manage reliable workflows with Runloom.',
    });
  }
}
