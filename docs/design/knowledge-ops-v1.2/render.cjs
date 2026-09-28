/** Export the SVG boards after running render.py. Requires playwright. */
const { chromium } = require('playwright');
const fs = require('fs');
const path = require('path');
const { pathToFileURL } = require('url');
const out = path.resolve(__dirname, '../../images');
const names = ['knowledge-ops-architecture','verification-system-design','verification-page-ui','verification-review-detail','verification-review-queue','verification-review-records'];
(async () => {
 const browser = await chromium.launch();
 try {
  for (const name of names) {
   const svg = path.join(out, name+'-v1.2.svg');
   const page = await browser.newPage({viewport:{width:1920,height:1630}, deviceScaleFactor:1.5});
   await page.goto(pathToFileURL(svg).href);
   await page.evaluate(() => document.fonts.ready);
   const issues = await page.evaluate(() => {
    const s=document.querySelector('svg'),v=s.viewBox.baseVal;
    const texts=[...s.querySelectorAll('text')].map(e=>({t:e.textContent,b:e.getBBox()}));
    const result=texts.filter(({b})=>b.x<0||b.y<0||b.x+b.width>v.width||b.y+b.height>v.height).map(({t})=>'Outside canvas: '+t);
    for(let i=0;i<texts.length;i++) for(let j=i+1;j<texts.length;j++) {
     const a=texts[i].b,b=texts[j].b;
     if(Math.min(a.x+a.width,b.x+b.width)-Math.max(a.x,b.x)>2 && Math.min(a.y+a.height,b.y+b.height)-Math.max(a.y,b.y)>2) result.push('Text overlap: '+texts[i].t+' / '+texts[j].t);
    }
    return result;
   });
   if(issues.length) throw new Error(name+':\n'+issues.join('\n'));
   await page.locator('svg').screenshot({path:svg.replace(/\.svg$/,'.png')});
   console.log(path.basename(svg)+' → PNG; text bounds and overlap checks passed');
   await page.close();
  }
 } finally {await browser.close();}
})().catch(error=>{console.error(error);process.exit(1)});
