let cleanup:(()=>void)|undefined;
export function setCleanup(fn:(()=>void)|undefined){cleanup=fn;}
export function hide(){cleanup?.();}
