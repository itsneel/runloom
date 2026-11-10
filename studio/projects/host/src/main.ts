// Federation must initialize its shared scope before Angular is imported.
import('./app/bootstrap').catch(error => console.error('Studio bootstrap failed', error));
