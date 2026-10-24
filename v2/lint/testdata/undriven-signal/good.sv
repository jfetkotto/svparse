module top (input clk, input d, output logic q);
  logic a, b, c, e, f;
  logic init = 1'b0;
  wire w = d;
  supply1 vdd;
  logic [1:0] split;
  logic hier;
  always_ff @(posedge clk) a <= d;
  assign b = a;
  leaf u(.d(b), .q(c));
  leaf v(.d(c), .q(e));
  initial f = 1;
  assign split[0] = 1'b1;
  assign split[1] = 1'b0;
  always_ff @(posedge clk) q <= a ^ b ^ c ^ e ^ f ^ init ^ w ^ vdd ^ split[0] ^ sub.out ^ hier;
  sub sub();
  initial sub.out = 0;
  assign hier = sub.out;
endmodule
module leaf(input d, output q); assign q = d; endmodule
module sub; logic out; endmodule
