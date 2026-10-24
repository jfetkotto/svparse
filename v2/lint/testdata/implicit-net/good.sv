module top;
  logic a, out_w, y;
  wire in_w;
  leaf u(.d(a), .q(out_w));
  leaf v(.d(in_w), .q());
  assign y = a;
endmodule
module leaf(input d, output q);
endmodule
