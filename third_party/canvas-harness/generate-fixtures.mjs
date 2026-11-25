// Run with Node 24+. Execute the pinned upstream path builders directly.
import { writeFileSync } from 'node:fs';
import {pathToFileURL} from 'node:url';
import * as paths from './reference/paths.ts';
import {drawArrowhead} from './reference/arrowhead.ts';
const rows = [];
for (const [shape, fn] of Object.entries({rect: paths.excalidrawRoundedRectPath, ellipse: paths.ellipsePath, diamond: paths.diamondPath, tag: paths.tagPath, 'thought-cloud': paths.thoughtCloudPath})) {
  for (const [W,H,Radius] of [[180,110,12],[30,24,8],[120,200,0]]) {
    rows.push({Shape:shape,W,H,Radius,Path:fn(0,0,W,H,Radius)});
  }
}
writeFileSync(new URL('../../internal/whiteboard/testdata/dim0-paths.json',import.meta.url),JSON.stringify(rows,null,2)+'\n');
const heads=[];
for(const kind of ['arrow','arrow-filled','barb']) {
  for(const width of [1,2,6]) {
    const ops=[];
    const ctx={save(){},restore(){},beginPath(){},stroke(){},fill(){},moveTo(...Data){ops.push({Op:'move',Data})},lineTo(...Data){ops.push({Op:'lineTo',Data})},closePath(){ops.push({Op:'close'})}};
    drawArrowhead(ctx,kind,{x:100,y:80},{x:.6,y:.8},'#475569',width);
    heads.push({kind,width,ops});
  }
}
writeFileSync(new URL('../../internal/whiteboard/testdata/dim0-arrowheads.json',import.meta.url),JSON.stringify(heads,null,2)+'\n');
// Optional verified perfect-freehand 1.2.3 package, matching dim0's dependency.
if(process.argv[2]) {
  const {getStroke}=await import(pathToFileURL(process.argv[2]));
  const cases=[];
  for(const points of [[[0,0],[160,80]],[[0,0],[30,5],[60,20],[90,60],[140,60]],[[0,0],[1,1],[2,0]]]) {
    for(const width of [1,2,6]) {
      const size=Math.max(1.2,width)*1.3,taper=Math.min(24,size*3);
      const input=points.map(([x,y],i)=>[x,y,.35+.55*Math.sin(Math.PI*i/(points.length-1))]);
      cases.push({points,width,ring:getStroke(input,{size,thinning:.55,smoothing:.6,streamline:.55,simulatePressure:false,last:true,start:{taper,cap:true},end:{taper,cap:true}})});
    }
  }
  writeFileSync(new URL('../../internal/whiteboard/testdata/dim0-brush.json',import.meta.url),JSON.stringify(cases,null,2)+'\n');
}
