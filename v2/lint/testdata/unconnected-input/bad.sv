module leaf (input logic clk, input logic d, output logic q);
  assign q = d & clk;
endmodule
module top (input logic clk, input logic a, output logic y);
  leaf u0 (.clk(clk), .q(y));                   // want
  leaf u1 (clk);                                // want
  leaf u2 (.clk(clk), .d(), .q(y));             // want
  leaf u_arr [1:0] (.clk(clk), .q(y));          // want
endmodule
